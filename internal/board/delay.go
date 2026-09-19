package board

import (
	"context"
	"time"
)

// ParseDelay reads a board's boardDelay setting, falling back to def when it
// is empty or doesn't read as a positive duration. Several boards used to set
// the fallback and then overwrite it with the zero that failed to parse.
func ParseDelay(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// DelaySetter is a board whose display time can be changed while it runs.
type DelaySetter interface {
	BoardDelay() time.Duration
	SetBoardDelay(time.Duration)
}

// DelayMinimum is a board that won't show for less than some time.
type DelayMinimum interface {
	MinBoardDelay() time.Duration
}

// Hold waits until d has passed since start, or ctx is done. A board that shows
// several things in turn times each one from when it started drawing it, not
// from when it finished, so the drawing comes out of the display time instead
// of adding to it: every item shows for its display time, however long it took
// to draw.
func Hold(ctx context.Context, start time.Time, d time.Duration) error {
	t := time.NewTimer(time.Until(start.Add(d)))
	defer t.Stop()

	select {
	case <-ctx.Done():
		return context.Canceled
	case <-t.C:
		return nil
	}
}
