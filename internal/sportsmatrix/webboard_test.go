package sportsmatrix

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
)

// fakeBrowser puts an executable called name on a PATH of its own, so the
// launcher finds it instead of whatever browser the machine running the tests
// has. The PATH holds nothing else, so the script can only use builtins and
// absolute paths.
func fakeBrowser(t *testing.T, name string, script string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755))
	t.Setenv("PATH", dir)

	return dir
}

func webBoardMatrix(t *testing.T) *SportsMatrix {
	t.Helper()

	me, err := user.Current()
	require.NoError(t, err)

	return &SportsMatrix{
		cfg:            &Config{WebBoardUser: me.Username, HTTPListenPort: 18202},
		log:            zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel)),
		webBoardIsOn:   atomic.NewBool(false),
		webBoardSettle: 300 * time.Millisecond,
	}
}

func processGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// nolint: paralleltest // t.Setenv
func TestWebBoardLaunch(t *testing.T) {
	t.Run("starts without waiting for the browser to exit", func(t *testing.T) {
		dir := fakeBrowser(t, "chromium", `echo "$@" > "$0.args"; echo $$ > "$0.pid"; exec /bin/sleep 60`)
		s := webBoardMatrix(t)
		t.Cleanup(s.stopWebBoard)

		start := time.Now()
		require.NoError(t, s.startWebBoard())
		require.Less(t, time.Since(start), 5*time.Second, "startWebBoard waited on the browser")
		require.True(t, s.webBoardIsOn.Load())

		args, err := os.ReadFile(filepath.Join(dir, "chromium.args"))
		require.NoError(t, err)
		require.Contains(t, string(args), "--kiosk --app=http://localhost:18202/board")

		raw, err := os.ReadFile(filepath.Join(dir, "chromium.pid"))
		require.NoError(t, err)
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		require.NoError(t, err)

		// a second start while one is running is a no-op, not a second browser
		require.NoError(t, s.startWebBoard())

		s.stopWebBoard()
		require.False(t, s.webBoardIsOn.Load())
		require.Eventually(t, func() bool { return processGone(pid) }, 10*time.Second, 50*time.Millisecond,
			"stopping the web board should close the browser")
	})

	t.Run("finds the Raspberry Pi OS name too", func(t *testing.T) {
		fakeBrowser(t, "chromium-browser", `exec /bin/sleep 60`)
		s := webBoardMatrix(t)
		t.Cleanup(s.stopWebBoard)

		require.NoError(t, s.startWebBoard())
		require.True(t, s.webBoardIsOn.Load())
	})

	t.Run("no browser is an error straight away", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		s := webBoardMatrix(t)

		start := time.Now()
		err := s.startWebBoard()
		require.ErrorIs(t, err, errNoBrowser)
		require.Contains(t, err.Error(), "chromium")
		require.Less(t, time.Since(start), time.Second)
		require.False(t, s.webBoardIsOn.Load())

		// nor does anything retry it, since installing a browser is not
		// something waiting will do
		start = time.Now()
		s.startWebBoardWithRetry(context.Background())
		require.Less(t, time.Since(start), time.Second)
	})

	t.Run("a browser that quits at once says why", func(t *testing.T) {
		fakeBrowser(t, "chromium", `echo "Missing X server or \$DISPLAY" >&2; exit 1`)
		s := webBoardMatrix(t)

		err := s.startWebBoard()
		require.Error(t, err)
		require.Contains(t, err.Error(), "Missing X server or $DISPLAY")
		require.False(t, s.webBoardIsOn.Load())
	})

	t.Run("a browser closed later is reported off", func(t *testing.T) {
		fakeBrowser(t, "chromium", `/bin/sleep 1; exit 0`)
		s := webBoardMatrix(t)

		require.NoError(t, s.startWebBoard())
		require.True(t, s.webBoardIsOn.Load())
		require.Eventually(t, func() bool { return !s.webBoardIsOn.Load() }, 10*time.Second, 50*time.Millisecond)
	})
}

// The dashboard's Start web board button calls SetStatus. It used to hang for
// about 50 seconds when the browser was missing, then report success, and would
// have hung for the life of the browser when it was not.
//
// nolint: paralleltest // t.Setenv
func TestWebBoardOverHTTP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))
	me, err := user.Current()
	require.NoError(t, err)

	cfg := &Config{ServeWebUI: false, HTTPListenPort: 18203, WebBoardWidth: 1, WebBoardUser: me.Username}
	cfg.Defaults()

	canvas := board.NewBlankCanvas(1, 1, logger)
	canvas.Enable()

	b := &TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()}
	b.enabler.Enable()

	dir := fakeBrowser(t, "chromium", `exec /bin/sleep 60`)

	s, err := New(ctx, logger, cfg, []board.Canvas{canvas}, b)
	require.NoError(t, err)
	s.webBoardSettle = 300 * time.Millisecond
	defer s.Close()

	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = s.Serve(ctx)
	}()
	t.Cleanup(func() {
		s.stopWebBoard()
		cancel()
		select {
		case <-served:
		case <-time.After(10 * time.Second):
			t.Error("timed out waiting for Serve to return")
		}
	})

	select {
	case <-s.isServing:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the matrix to serve")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	call := func(method string, path string, body string) (int, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, "http://localhost:18203"+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(out)
	}
	webBoardOn := func() bool {
		t.Helper()
		code, body := call(http.MethodPost, "/matrix.v1.Sportsmatrix/GetStatus", "{}")
		require.Equal(t, http.StatusOK, code, body)
		var st struct {
			WebboardOn bool `json:"webboard_on"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &st))
		return st.WebboardOn
	}

	code, body := call(http.MethodPost, "/matrix.v1.Sportsmatrix/SetStatus", `{"screen_on":true,"webboard_on":true}`)
	require.Equal(t, http.StatusOK, code, body)
	require.True(t, webBoardOn())

	code, body = call(http.MethodPost, "/matrix.v1.Sportsmatrix/SetStatus", `{"screen_on":true,"webboard_on":false}`)
	require.Equal(t, http.StatusOK, code, body)
	require.False(t, webBoardOn())

	// now with nothing to launch
	require.NoError(t, os.Remove(filepath.Join(dir, "chromium")))

	start := time.Now()
	code, body = call(http.MethodPost, "/matrix.v1.Sportsmatrix/SetStatus", `{"screen_on":true,"webboard_on":true}`)
	require.Less(t, time.Since(start), 2*time.Second)
	require.Equal(t, http.StatusPreconditionFailed, code, body)
	require.Contains(t, body, "could not start the web board")
	require.Contains(t, body, "chromium-browser")
	require.False(t, webBoardOn())

	// The old handlers blocked forever holding the lock ListBoards takes.
	code, body = call(http.MethodGet, "/api/webboardon", "")
	require.Equal(t, http.StatusPreconditionFailed, code, body)
	code, _ = call(http.MethodGet, "/api/webboardoff", "")
	require.Equal(t, http.StatusOK, code)
	code, body = call(http.MethodPost, "/matrix.v1.Sportsmatrix/ListBoards", "{}")
	require.Equal(t, http.StatusOK, code, body)
}
