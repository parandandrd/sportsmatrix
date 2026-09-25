package tvmaze

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// server serves the saved TVmaze answers, counting requests by path.
func server(t *testing.T) (*Client, map[string]int) {
	t.Helper()

	var lock sync.Mutex
	hits := make(map[string]int)
	files := map[string]string{
		"/shows/28152":          "show_28152.json",
		"/shows/82759":          "show_82759.json",
		"/shows/84":             "show_84.json",
		"/shows/82759/episodes": "episodes_82759.json",
		"/search/shows":         "search_911.json",
		"/singlesearch/shows":   "singlesearch_survivor.json",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		lock.Lock()
		hits[req.URL.Path]++
		lock.Unlock()

		if req.URL.Path == "/singlesearch/shows" && req.URL.Query().Get("q") != "survivor" {
			http.NotFound(w, req)
			return
		}
		name, ok := files[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		http.ServeFile(w, req, "testdata/"+name)
	}))
	t.Cleanup(srv.Close)

	return New(srv.Client(), srv.URL), hits
}

// fetched is when the testdata was saved.
var fetched = time.Date(2026, 9, 25, 10, 0, 0, 0, time.FixedZone("CDT", -5*3600))

func TestShow(t *testing.T) {
	t.Parallel()
	c, _ := server(t)

	show, next, err := c.Show(context.Background(), 82759)
	require.NoError(t, err)
	require.Equal(t, Show{ID: 82759, Name: "9-1-1: Nashville", Network: "ABC", Premiered: "2025", Status: "Running"}, show)
	require.NotNil(t, next)
	require.Equal(t, 2, next.Season)
	require.Equal(t, 1, next.Number)
	require.True(t, next.Premiere())
	require.True(t, next.Timed)
	require.Equal(t, time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC), next.Airs.UTC())
	require.Equal(t, 60*time.Minute, next.Runtime)

	_, _, err = c.Show(context.Background(), 1)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestUpcoming(t *testing.T) {
	t.Parallel()
	c, hits := server(t)
	ctx := context.Background()

	// the week from when the testdata was saved has nothing
	show, eps, err := c.Upcoming(ctx, 82759, fetched, fetched.AddDate(0, 0, 7))
	require.NoError(t, err)
	require.Equal(t, "9-1-1: Nashville", show.Name)
	require.Empty(t, eps)
	require.Zero(t, hits["/shows/82759/episodes"], "the next episode says there is nothing, so the list isn't fetched")

	// three weeks takes in the premiere
	_, eps, err = c.Upcoming(ctx, 82759, fetched, fetched.AddDate(0, 0, 21))
	require.NoError(t, err)
	require.Len(t, eps, 1)
	require.Equal(t, 2, eps[0].Season)
	require.True(t, eps[0].Premiere())
	require.Equal(t, 1, hits["/shows/82759/episodes"])

	// Family Guy is off until February
	show, eps, err = c.Upcoming(ctx, 84, fetched, fetched.AddDate(0, 0, 7))
	require.NoError(t, err)
	require.Equal(t, "Family Guy", show.Name)
	require.Equal(t, "FOX", show.Network)
	require.Empty(t, eps)
}

func TestSearch(t *testing.T) {
	t.Parallel()
	c, _ := server(t)

	shows, err := c.Search(context.Background(), "911")
	require.NoError(t, err)
	require.NotEmpty(t, shows)
	require.Equal(t, "9-1-1", shows[0].Name)
	require.Equal(t, 28152, shows[0].ID)
	require.Equal(t, "ABC", shows[0].Network)
	require.Equal(t, "2018", shows[0].Premiered)
}

func TestFind(t *testing.T) {
	t.Parallel()
	c, _ := server(t)

	show, err := c.Find(context.Background(), "survivor")
	require.NoError(t, err)
	require.Equal(t, "Survivor", show.Name)
	require.Positive(t, show.ID)

	_, err = c.Find(context.Background(), "no such show at all")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestEntry(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]Entry{
		"Family Guy (tvmaze 84)":             {Name: "Family Guy", ID: 84},
		"  9-1-1: Nashville (tvmaze 82759) ": {Name: "9-1-1: Nashville", ID: 82759},
		"Survivor":                           {Name: "Survivor"},
		"Doctor Who (2023)":                  {Name: "Doctor Who (2023)"},
		"Doctor Who (2023) (tvmaze 70123)":   {Name: "Doctor Who (2023)", ID: 70123},
		"Bad (tvmaze 0)":                     {Name: "Bad (tvmaze 0)"},
	} {
		require.Equal(t, want, ParseEntry(in), in)
	}

	require.Equal(t, "Family Guy (tvmaze 84)", Entry{Name: "Family Guy", ID: 84}.String())
	require.Equal(t, "Survivor", Entry{Name: "Survivor"}.String())
}
