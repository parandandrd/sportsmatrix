package board

import (
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
