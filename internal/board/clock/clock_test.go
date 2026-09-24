package clock

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
)

// slowCanvas takes a while to put a frame on the panel, and notes when each
// frame finished.
type slowCanvas struct {
	*board.BlankCanvas
	lock     sync.Mutex
	rendered []time.Time
}

func (c *slowCanvas) Render(ctx context.Context) error {
	time.Sleep(300 * time.Millisecond)

	c.lock.Lock()
	defer c.lock.Unlock()
	c.rendered = append(c.rendered, time.Now())

	return nil
}

// TestClockStopsDrawingBeforeReturning covers the clock's drawing goroutine
// outliving Render. The next board starts drawing the moment Render returns,
// so a clock frame still going out then lands on top of it. The clock draws
// its first frame at half a second and this canvas takes 300ms to show it, so
// with a 600ms display time the frame is still being drawn when the time is
// up.
func TestClockStopsDrawingBeforeReturning(t *testing.T) {
	t.Parallel()

	cfg := &Config{StartEnabled: atomic.NewBool(true)}
	cfg.SetDefaults()

	c, err := New(cfg, zap.NewNop())
	require.NoError(t, err)
	c.SetBoardDelay(600 * time.Millisecond)

	canvas := &slowCanvas{BlankCanvas: board.NewBlankCanvas(64, 32, zap.NewNop())}

	require.NoError(t, c.Render(context.Background(), canvas))
	returned := time.Now()

	// Anything still running would finish in here.
	time.Sleep(500 * time.Millisecond)

	canvas.lock.Lock()
	defer canvas.lock.Unlock()

	require.NotEmpty(t, canvas.rendered, "the clock never drew")
	for _, at := range canvas.rendered {
		require.False(t, at.After(returned),
			"the clock drew a frame %s after Render returned", at.Sub(returned))
	}
}
