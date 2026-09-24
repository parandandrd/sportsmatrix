package sportsmatrix

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
)

// emptyBoard has nothing to show, like a sport board with no games today: its
// Render returns an error straight away.
type emptyBoard struct {
	TestBoard
	renders *atomic.Int64
}

func (b *emptyBoard) Name() string { return "Empty Board" }

func (b *emptyBoard) Render(ctx context.Context, canvas board.Canvas) error {
	b.renders.Inc()
	return errors.New("no games scheduled")
}

// TestNothingToShowDoesNotSpin covers every enabled board having nothing to
// show. The serve loop went straight round again, and managed some 200,000
// passes a second, each logging an error. It should wait between passes, and
// still answer a jump promptly while it does.
//
// nolint: paralleltest
func TestNothingToShowDoesNotSpin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))

	cfg := &Config{ServeWebUI: false, HTTPListenPort: 18205}
	cfg.Defaults()

	canvas := board.NewBlankCanvas(1, 1, logger)
	canvas.Enable()

	b := &emptyBoard{
		TestBoard: TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()},
		renders:   atomic.NewInt64(0),
	}
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

	require.Eventually(t, func() bool { return b.renders.Load() > 0 },
		10*time.Second, 10*time.Millisecond, "the board was never rendered")

	before := b.renders.Load()
	time.Sleep(2 * time.Second)
	passes := b.renders.Load() - before
	require.LessOrEqual(t, passes, int64(2), "the serve loop rendered a board with nothing to show %d times in 2s", passes)

	// A jump turns the screen off and on, which should cut the wait short.
	before = b.renders.Load()
	require.NoError(t, s.JumpTo(ctx, b.Name()))
	require.Eventually(t, func() bool { return b.renders.Load() > before },
		2*time.Second, 10*time.Millisecond, "the jump waited out the pause")
}
