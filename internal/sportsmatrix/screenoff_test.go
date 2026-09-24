package sportsmatrix

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
)

// recordingCanvas notes each frame put on the panel and each time it is
// cleared, in order.
type recordingCanvas struct {
	*board.BlankCanvas
	lock   sync.Mutex
	events []string
}

func (c *recordingCanvas) record(event string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.events = append(c.events, event)
}

func (c *recordingCanvas) Render(ctx context.Context) error {
	c.record("render")
	return nil
}

func (c *recordingCanvas) Clear() error {
	c.record("clear")
	return nil
}

func (c *recordingCanvas) seen() []string {
	c.lock.Lock()
	defer c.lock.Unlock()
	return append([]string(nil), c.events...)
}

// lingeringBoard puts one more frame on the panel after it is told to stop,
// as a board part way through drawing one does.
type lingeringBoard struct {
	TestBoard
}

func (b *lingeringBoard) Render(ctx context.Context, canvas board.Canvas) error {
	_ = canvas.Render(ctx)
	<-ctx.Done()
	time.Sleep(300 * time.Millisecond)
	_ = canvas.Render(ctx)

	return nil
}

// TestScreenOffClearsAfterTheBoardStops covers ScreenOff clearing the panel
// the moment it told the board to stop. A board still finishing a frame then
// drew it over the blank screen, and it stayed lit until the screen came back
// on.
//
// nolint: paralleltest
func TestScreenOffClearsAfterTheBoardStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))

	cfg := &Config{ServeWebUI: false, HTTPListenPort: 18207}
	cfg.Defaults()

	canvas := &recordingCanvas{BlankCanvas: board.NewBlankCanvas(1, 1, logger)}
	canvas.Enable()

	b := &lingeringBoard{TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()}}
	b.enabler.Enable()

	s, err := New(ctx, logger, cfg, []board.Canvas{canvas}, b)
	require.NoError(t, err)

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(ctx) }()

	t.Cleanup(func() {
		cancel()
		select {
		case <-serveErr:
		case <-time.After(10 * time.Second):
			t.Error("timed out waiting for Serve to return")
		}
	})

	require.Eventually(t, func() bool { return len(canvas.seen()) > 0 },
		10*time.Second, 10*time.Millisecond, "the board never drew")

	require.NoError(t, s.ScreenOff(ctx))

	// Long enough for the board's last frame, and for the clear after it.
	time.Sleep(time.Second)

	events := canvas.seen()
	require.Equal(t, "clear", events[len(events)-1],
		"the panel was left showing a frame after the screen went off: %v", events)
}

// TestScreenOffBeforeServe covers the screen being turned off before Serve
// has started, as a schedule can at startup. ScreenOff made the boards' next
// context from Serve's, which was still nil, and panicked.
//
// nolint: paralleltest
func TestScreenOffBeforeServe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))

	cfg := &Config{ServeWebUI: false, HTTPListenPort: 18208}
	cfg.Defaults()

	canvas := board.NewBlankCanvas(1, 1, logger)
	canvas.Enable()

	b := &holdBoard{
		TestBoard: TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()},
		renders:   atomic.NewInt64(0),
	}
	b.enabler.Enable()

	s, err := New(ctx, logger, cfg, []board.Canvas{canvas}, b)
	require.NoError(t, err)

	require.NoError(t, s.ScreenOff(ctx))

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(ctx) }()

	t.Cleanup(func() {
		cancel()
		select {
		case <-serveErr:
		case <-time.After(10 * time.Second):
			t.Error("timed out waiting for Serve to return")
		}
	})

	// Serve takes a second to start its HTTP server; give it time to reach
	// the boards if it was going to.
	time.Sleep(2 * time.Second)
	require.Zero(t, b.renders.Load(), "a board was shown while the screen was off")

	require.NoError(t, s.ScreenOn(ctx))
	require.Eventually(t, func() bool { return b.renders.Load() > 0 },
		5*time.Second, 10*time.Millisecond, "the board wasn't shown once the screen came on")
}
