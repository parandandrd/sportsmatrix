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

// countingEnabler records how often the serve loop asks whether a board is on.
type countingEnabler struct {
	board.Enabler
	checks *atomic.Int64
}

func (c *countingEnabler) Enabled() bool {
	c.checks.Inc()
	return c.Enabler.Enabled()
}

// TestAllDisabledDoesNotSpin covers the serve loop's behavior when every board
// is turned off, which the web UI can do in a couple of clicks. The loop used
// to clear the canvases and then `continue` with nothing to wait on, re-running
// allDisabled() tens of millions of times a second and pinning a core until a
// board came back. It should sleep instead, and still wake promptly when one
// is re-enabled.
//
// nolint: paralleltest
func TestAllDisabledDoesNotSpin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))

	cfg := &Config{ServeWebUI: false, HTTPListenPort: 18201}
	cfg.Defaults()

	canvas := board.NewBlankCanvas(1, 1, logger)
	canvas.Enable()

	checks := atomic.NewInt64(0)
	en := &countingEnabler{Enabler: enabler.New(), checks: checks}
	en.Disable()

	b := &TestBoard{
		log:         logger,
		hasRendered: atomic.NewBool(false),
		tester:      t,
		enabler:     en,
	}

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

	// Wait for the loop to reach the all-disabled branch at least once.
	require.Eventually(t, func() bool { return checks.Load() > 0 },
		10*time.Second, 10*time.Millisecond,
		"serve loop never checked whether boards were enabled")

	// Now measure. A sleeping loop re-checks a handful of times at most; the
	// spinning one managed ~19 million in this window.
	before := checks.Load()
	time.Sleep(300 * time.Millisecond)
	spun := checks.Load() - before

	require.Less(t, spun, int64(100),
		"serve loop is spinning on allDisabled(): %d checks in 300ms", spun)

	// And it has to actually wake up, or the fix is just a hang.
	require.False(t, b.HasRendered(), "board rendered while disabled")
	en.Enable()

	require.Eventually(t, b.HasRendered, 10*time.Second, 20*time.Millisecond,
		"serve loop did not wake up after a board was re-enabled")
}
