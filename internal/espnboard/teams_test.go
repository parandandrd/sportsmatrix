package espnboard

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A sport board renders to every canvas at once, and each render calls
// GetTeams. This crashed the service on the real Pi with "fatal error:
// concurrent map writes", and before that grew allTeamIDs by the whole league
// on every call.
func TestGetTeamsConcurrent(t *testing.T) {
	t.Parallel()

	l, err := GetLeaguer("nhl")
	require.NoError(t, err)

	e, err := New(context.Background(), l, zap.NewNop(), nil, nil)
	require.NoError(t, err)

	// already cached, so nothing here reaches for the network
	e.teams = []*Team{
		{ID: "1", Conference: &Conference{Abbreviation: "EAST_ATL"}},
		{ID: "2", Conference: &Conference{Abbreviation: "EAST_ATL"}},
		{ID: "3", Conference: &Conference{Abbreviation: "WEST_PAC"}},
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				teams, err := e.GetTeams(context.Background())
				assert.NoError(t, err)
				assert.Len(t, teams, 3)
			}
		}()
	}
	wg.Wait()

	require.ElementsMatch(t, []string{"1", "2", "3"}, e.GetWatchTeams([]string{"ALL"}, ""))
	require.Len(t, e.conferenceNames, 2)
}
