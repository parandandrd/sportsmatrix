package sportsmatrix

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
)

// holdBoard shows until it is told to stop, as a board does for its display
// time.
type holdBoard struct {
	TestBoard
	renders *atomic.Int64
}

func (b *holdBoard) Render(ctx context.Context, canvas board.Canvas) error {
	b.renders.Inc()
	<-ctx.Done()

	return nil
}

// TestJumpReturnsPromptly covers a jump taking ten seconds. JumpTo turns the
// screen off and back on, and turning it on waited for the serve loop to take
// word of it -- which the loop only does if it saw the screen off, and it
// usually looked after the screen was already back on. The jump itself
// happened straight away, but the call held on, and every other jump was
// ignored, until the wait timed out.
//
// nolint: paralleltest
func TestJumpReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))

	cfg := &Config{ServeWebUI: false, HTTPListenPort: 18206}
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

	for i := 0; i < 3; i++ {
		before := b.renders.Load()
		start := time.Now()
		require.NoError(t, s.JumpTo(ctx, b.Name()))
		require.Less(t, time.Since(start), 2*time.Second, "jump %d took %s", i, time.Since(start))
		require.Eventually(t, func() bool { return b.renders.Load() > before },
			2*time.Second, 10*time.Millisecond, "jump %d never showed the board", i)
	}
}
