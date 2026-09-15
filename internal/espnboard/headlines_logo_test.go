package espnboard

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Not every league ships a logo under assets/league_logos. The sport board and
// the headlines board both decide whether to draw without one by testing this
// error with errors.Is, so if GetLogo ever stops wrapping it, both silently go
// back to failing their render on every cycle.
func TestMissingLeagueLogoReportsNotExist(t *testing.T) {
	t.Parallel()

	nwsl, err := GetLeaguer("nwsl")
	require.NoError(t, err)

	_, err = NewHeadlines(nwsl, zap.NewNop()).GetLogo(context.Background())
	require.Error(t, err, "nwsl has no bundled logo asset")
	require.True(t, errors.Is(err, fs.ErrNotExist),
		"must be recognisable as a missing file, got: %v", err)
}

// And a league that does ship one still loads.
func TestPresentLeagueLogoLoads(t *testing.T) {
	t.Parallel()

	nhl, err := GetLeaguer("nhl")
	require.NoError(t, err)

	img, err := NewHeadlines(nhl, zap.NewNop()).GetLogo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, img)
}
