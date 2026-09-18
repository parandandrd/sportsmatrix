package board

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseDelay(t *testing.T) {
	t.Parallel()

	require.Equal(t, 15*time.Second, ParseDelay("15s", 10*time.Second))
	require.Equal(t, 90*time.Second, ParseDelay("1m30s", 10*time.Second))
	require.Equal(t, 10*time.Second, ParseDelay("", 10*time.Second))
	// these used to come out as zero on several boards
	require.Equal(t, 10*time.Second, ParseDelay("ten seconds", 10*time.Second))
	require.Equal(t, 10*time.Second, ParseDelay("-5s", 10*time.Second))
}

func TestHoldCountsFromStart(t *testing.T) {
	t.Parallel()

	// Half the display time already went on drawing, so only the rest is left.
	start := time.Now().Add(-100 * time.Millisecond)
	began := time.Now()
	require.NoError(t, Hold(context.Background(), start, 200*time.Millisecond))
	waited := time.Since(began)
	require.GreaterOrEqual(t, waited, 90*time.Millisecond)
	require.Less(t, waited, 180*time.Millisecond)

	// Drawing took longer than the whole display time: move straight on.
	began = time.Now()
	require.NoError(t, Hold(context.Background(), time.Now().Add(-time.Second), 200*time.Millisecond))
	require.Less(t, time.Since(began), 50*time.Millisecond)
}

func TestHoldCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, Hold(ctx, time.Now(), time.Hour), context.Canceled)
}
