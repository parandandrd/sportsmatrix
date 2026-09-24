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

// stubbornBoard takes a while to stop once it is told to.
type stubbornBoard struct {
	TestBoard
	finished *atomic.Bool
}

func (b *stubbornBoard) Render(ctx context.Context, canvas board.Canvas) error {
	<-ctx.Done()
	time.Sleep(300 * time.Millisecond)
	b.finished.Store(true)

	return ctx.Err()
}

// TestDoBoardWaitsForTheBoardToStop covers doBoard returning the moment its
// context was canceled, while the board was still drawing. The next board
// then started drawing into the same canvas alongside it.
//
// nolint: paralleltest
func TestDoBoardWaitsForTheBoardToStop(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))

	cfg := &Config{ServeWebUI: false}
	cfg.Defaults()

	canvas := board.NewBlankCanvas(1, 1, logger)
	canvas.Enable()

	b := &stubbornBoard{
		TestBoard: TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()},
		finished:  atomic.NewBool(false),
	}
	b.enabler.Enable()

	s, err := New(context.Background(), logger, cfg, []board.Canvas{canvas}, b)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	require.ErrorIs(t, s.doBoard(ctx, b), context.Canceled)
	require.True(t, b.finished.Load(), "doBoard returned while the board was still drawing")
}
