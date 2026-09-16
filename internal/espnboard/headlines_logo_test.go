package espnboard

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// noLogoLeaguer stands in for a league added without a logo asset. Every league
// in the tree ships one today, so this is the only way to keep the missing-asset
// path covered.
type noLogoLeaguer struct{}

func (n *noLogoLeaguer) League() string                { return "No Such League" }
func (n *noLogoLeaguer) APIPath() string               { return "nosuch/league" }
func (n *noLogoLeaguer) TeamEndpoints() []string       { return []string{} }
func (n *noLogoLeaguer) HTTPPathPrefix() string        { return "nosuchleague" }
func (n *noLogoLeaguer) HeadlinePath() string          { return "nosuch/league/news" }
func (n *noLogoLeaguer) HomeSideSwap() bool            { return false }
func (n *noLogoLeaguer) SetScoreboardQuery(url.Values) {}

// The sport board and the headlines board both decide whether to draw without a
// logo by testing this error with errors.Is, so if GetLogo ever stops wrapping
// it, both silently go back to failing their render on every cycle.
func TestMissingLeagueLogoReportsNotExist(t *testing.T) {
	t.Parallel()

	_, err := NewHeadlines(&noLogoLeaguer{}, zap.NewNop()).GetLogo(context.Background())
	require.Error(t, err, "a league with no bundled logo asset must error")
	require.True(t, errors.Is(err, fs.ErrNotExist),
		"must be recognisable as a missing file, got: %v", err)
}

// And a league that does ship one still loads.
func TestPresentLeagueLogoLoads(t *testing.T) {
	t.Parallel()

	for _, league := range []string{"nhl", "nwsl"} {
		t.Run(league, func(t *testing.T) {
			t.Parallel()

			l, err := GetLeaguer(league)
			require.NoError(t, err)

			img, err := NewHeadlines(l, zap.NewNop()).GetLogo(context.Background())
			require.NoError(t, err)
			require.NotNil(t, img)
			require.False(t, img.Bounds().Empty(), "decoded logo has no pixels")
		})
	}
}
