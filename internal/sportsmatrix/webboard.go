package sportsmatrix

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// webBoardBrowsers are the names chromium is installed under, in the order they
// are tried. Debian calls it chromium; Raspberry Pi OS shipped it as
// chromium-browser until it moved to the Debian package.
var webBoardBrowsers = []string{"chromium", "chromium-browser"}

// errNoBrowser means there is nothing to launch, which no amount of retrying
// will fix.
var errNoBrowser = errors.New("chromium is not installed")

const (
	// defaultWebBoardSettle is how long a freshly started browser has to fall
	// over before it counts as running. Chromium with no display to draw on
	// quits within a second or two, and its complaint is the thing whoever
	// pressed the button needs to see.
	defaultWebBoardSettle = 3 * time.Second

	// A browser launched at boot can beat the desktop session it draws on, so
	// launches nobody is waiting on get a few tries.
	webBoardAttempts   = 10
	webBoardRetryDelay = 5 * time.Second
)

func findBrowser() (string, error) {
	for _, name := range webBoardBrowsers {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("%w (looked for %s)", errNoBrowser, strings.Join(webBoardBrowsers, " and "))
}

// webBoardCmd builds the browser command. The browser shows /board full screen
// on the display attached to the Pi, as WebBoardUser, since chromium refuses to
// run as root.
func (s *SportsMatrix) webBoardCmd(ctx context.Context) (*exec.Cmd, *tailBuffer, error) {
	browser, err := findBrowser()
	if err != nil {
		return nil, nil, err
	}

	u, err := user.Lookup(s.cfg.WebBoardUser)
	if err != nil {
		return nil, nil, err
	}

	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, nil, err
	}

	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, nil, err
	}

	cmd := exec.CommandContext(ctx, browser,
		"--kiosk",
		fmt.Sprintf("--app=http://localhost:%d/board", s.cfg.HTTPListenPort),
	)

	cmd.Env = append(os.Environ(),
		"DISPLAY=:0",
		"HOME="+u.HomeDir,
		"XAUTHORITY="+filepath.Join(u.HomeDir, ".Xauthority"),
	)

	// Only switch users when there is another user to switch to. An unprivileged
	// process cannot, and has no need to when it already is that user.
	if uid != os.Getuid() || gid != os.Getgid() {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uint32(uid),
				Gid: uint32(gid),
			},
		}
	}

	// Ask the browser to close before killing it.
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	out := &tailBuffer{max: 2048}
	cmd.Stdout = out
	cmd.Stderr = out

	return cmd, out, nil
}

// startWebBoard launches the browser and returns once it is running, or with
// the reason it is not. It used to wait for the browser to exit -- for as long
// as the board was up, holding webBoardLock -- and to retry for most of a
// minute when it could not start at all.
func (s *SportsMatrix) startWebBoard() error {
	s.webBoardLock.Lock()

	if s.webBoardIsOn.Load() {
		s.webBoardLock.Unlock()
		return nil
	}

	// The browser belongs to the service, not to whichever request started it:
	// a request context is canceled, and the browser killed, as soon as the
	// response is written.
	parent := s.serveContext
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)

	cmd, out, err := s.webBoardCmd(ctx)
	if err == nil {
		s.log.Info("launching web board",
			zap.String("command", strings.Join(cmd.Args, " ")),
			zap.String("user", s.cfg.WebBoardUser),
		)
		err = cmd.Start()
	}
	if err != nil {
		s.webBoardLock.Unlock()
		cancel()
		return err
	}

	s.webBoardCancel = cancel
	s.webBoardIsOn.Store(true)
	s.webBoardLock.Unlock()

	var (
		waitErr error
		stopped bool
	)
	exited := make(chan struct{})

	go func() {
		err := cmd.Wait()

		s.webBoardLock.Lock()
		// A canceled context means stopWebBoard has been here, and a newer
		// browser may already be up.
		stopped = ctx.Err() != nil
		if !stopped {
			cancel()
			s.webBoardIsOn.Store(false)
		}
		s.webBoardLock.Unlock()

		waitErr = err
		close(exited)

		if !stopped {
			s.log.Warn("web board browser exited",
				zap.Error(err),
				zap.String("output", out.String()),
			)
		}
	}()

	select {
	case <-exited:
		if stopped || waitErr == nil {
			return nil
		}
		return fmt.Errorf("%s quit as soon as it started (%w): %s",
			filepath.Base(cmd.Path), waitErr, strings.TrimSpace(out.String()))
	case <-time.After(s.webBoardSettle):
		return nil
	}
}

// startWebBoardWithRetry is for launches nobody is waiting on: at boot, and
// when the screen comes back on.
func (s *SportsMatrix) startWebBoardWithRetry(ctx context.Context) {
	for attempt := 1; ; attempt++ {
		err := s.startWebBoard()
		if err == nil {
			return
		}

		s.log.Error("failed to launch web board",
			zap.Error(err),
			zap.Int("attempt", attempt),
		)

		if attempt >= webBoardAttempts || errors.Is(err, errNoBrowser) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(webBoardRetryDelay):
		}
	}
}

func (s *SportsMatrix) stopWebBoard() {
	s.webBoardLock.Lock()
	defer s.webBoardLock.Unlock()
	if !s.webBoardIsOn.Load() {
		return
	}
	s.webBoardCancel()
	s.webBoardIsOn.Store(false)
}

// tailBuffer keeps the last max bytes written to it. Chromium is chatty; only
// the end of what it said is worth logging.
type tailBuffer struct {
	max int
	buf []byte
	sync.Mutex
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.Lock()
	defer t.Unlock()

	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}

	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.Lock()
	defer t.Unlock()

	return string(t.buf)
}
