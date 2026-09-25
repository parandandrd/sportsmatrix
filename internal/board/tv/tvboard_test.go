package tvboard

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/tvmaze"
)

var testNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.FixedZone("CDT", -5*3600))

var (
	nashville = tvmaze.Show{ID: 82759, Name: "9-1-1: Nashville", Network: "ABC", Premiered: "2025"}
	nineOne   = tvmaze.Show{ID: 28152, Name: "9-1-1", Network: "ABC", Premiered: "2018"}
	familyGuy = tvmaze.Show{ID: 84, Name: "Family Guy", Network: "FOX", Premiered: "1999"}
	survivor  = tvmaze.Show{ID: 2, Name: "Survivor", Network: "CBS", Premiered: "2000"}
)

// fakeAPI has a few shows with episodes around testNow.
type fakeAPI struct {
	lock     sync.Mutex
	upcoming map[int]int
	fail     error
	finds    int
}

func (f *fakeAPI) episodes(id int) []tvmaze.Episode {
	switch id {
	case nashville.ID:
		return []tvmaze.Episode{
			{Show: nashville, Season: 2, Number: 1, Airs: testNow.Add(8 * time.Hour), Timed: true, Runtime: time.Hour},
		}
	case nineOne.ID:
		return []tvmaze.Episode{
			{Show: nineOne, Season: 10, Number: 1, Airs: testNow.Add(7 * time.Hour), Timed: true, Runtime: time.Hour},
			{Show: nineOne, Season: 10, Number: 2, Airs: testNow.Add(7*time.Hour + 7*24*time.Hour), Timed: true, Runtime: time.Hour},
		}
	case survivor.ID:
		return []tvmaze.Episode{
			{Show: survivor, Season: 49, Number: 4, Airs: testNow.Add(-30 * time.Minute), Timed: true, Runtime: time.Hour},
		}
	}
	return nil
}

func (f *fakeAPI) show(id int) (tvmaze.Show, bool) {
	for _, s := range []tvmaze.Show{nashville, nineOne, familyGuy, survivor} {
		if s.ID == id {
			return s, true
		}
	}
	return tvmaze.Show{}, false
}

func (f *fakeAPI) Upcoming(ctx context.Context, id int, from, to time.Time) (tvmaze.Show, []tvmaze.Episode, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.upcoming == nil {
		f.upcoming = make(map[int]int)
	}
	f.upcoming[id]++
	if f.fail != nil {
		return tvmaze.Show{}, nil, f.fail
	}
	s, ok := f.show(id)
	if !ok {
		return tvmaze.Show{}, nil, tvmaze.ErrNotFound
	}
	var out []tvmaze.Episode
	for _, e := range f.episodes(id) {
		if !e.Airs.Before(from) && !e.Airs.After(to) {
			out = append(out, e)
		}
	}
	return s, out, nil
}

func (f *fakeAPI) Show(ctx context.Context, id int) (tvmaze.Show, *tvmaze.Episode, error) {
	s, ok := f.show(id)
	if !ok {
		return tvmaze.Show{}, nil, fmt.Errorf("%w with the ID %d", tvmaze.ErrNotFound, id)
	}
	return s, nil, nil
}

func (f *fakeAPI) Find(ctx context.Context, name string) (tvmaze.Show, error) {
	f.lock.Lock()
	f.finds++
	f.lock.Unlock()
	if name == "survivor" {
		return survivor, nil
	}
	return tvmaze.Show{}, tvmaze.ErrNotFound
}

func (f *fakeAPI) Search(ctx context.Context, query string) ([]tvmaze.Show, error) {
	return []tvmaze.Show{nineOne, nashville}, nil
}

// frameCanvas keeps a copy of each frame it is asked to show.
type frameCanvas struct {
	*board.BlankCanvas
	frames []*image.RGBA
}

func newFrameCanvas() *frameCanvas {
	return &frameCanvas{BlankCanvas: board.NewBlankCanvas(64, 32, zap.NewNop())}
}

func (c *frameCanvas) Render(ctx context.Context) error {
	img := image.NewRGBA(c.Bounds())
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, c.At(x, y))
		}
	}
	c.frames = append(c.frames, img)
	return c.BlankCanvas.Clear()
}

func testBoard(t *testing.T, shows ...string) (*Board, *fakeAPI) {
	t.Helper()
	cfg := &Config{StartEnabled: atomic.NewBool(true), Shows: shows}
	cfg.SetDefaults()
	api := &fakeAPI{}
	b, err := newBoard(cfg, api, zap.NewNop())
	require.NoError(t, err)
	b.now = func() time.Time { return testNow }
	b.SetBoardDelay(time.Millisecond)
	return b, api
}

func TestDefaults(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	cfg.SetDefaults()
	require.Equal(t, 7, cfg.LookaheadDays)
	require.Equal(t, defaultDelay, cfg.boardDelay.Load())
	require.False(t, cfg.StartEnabled.Load())

	cfg = &Config{LookaheadDays: 400}
	cfg.SetDefaults()
	require.Equal(t, maxLookahead, cfg.LookaheadDays)
}

func TestNoShowsSkips(t *testing.T) {
	t.Parallel()

	b, api := testBoard(t)
	canvas := newFrameCanvas()
	require.NoError(t, b.Render(context.Background(), canvas))
	require.Empty(t, canvas.frames)
	require.Empty(t, api.upcoming)
}

func TestNothingThisWeekSkips(t *testing.T) {
	t.Parallel()

	b, _ := testBoard(t, "Family Guy (tvmaze 84)")
	canvas := newFrameCanvas()
	require.NoError(t, b.Render(context.Background(), canvas))
	require.Empty(t, canvas.frames)
}

func TestEpisodesInOrder(t *testing.T) {
	t.Parallel()

	b, api := testBoard(t, "9-1-1: Nashville (tvmaze 82759)", "Family Guy (tvmaze 84)", "9-1-1 (tvmaze 28152)", "survivor")
	eps := b.upcoming(context.Background())

	var got []string
	for _, e := range eps {
		got = append(got, fmt.Sprintf("%s S%dE%d", e.Show.Name, e.Season, e.Number))
	}
	require.Equal(t, []string{
		"Survivor S49E4", // on now
		"9-1-1 S10E1",
		"9-1-1: Nashville S2E1",
		// not next week's 9-1-1, which is seven hours past the seven days
	}, got)
	require.Equal(t, 1, api.finds, "a show named without an ID is looked up")

	canvas := newFrameCanvas()
	require.NoError(t, b.Render(context.Background(), canvas))
	require.Len(t, canvas.frames, 3)

	// asked again only once what it has is old
	require.NoError(t, b.Render(context.Background(), canvas))
	require.Equal(t, 1, api.upcoming[nashville.ID])
	require.Equal(t, 1, api.finds)

	b.now = func() time.Time { return testNow.Add(refreshEvery) }
	b.upcoming(context.Background())
	require.Equal(t, 2, api.upcoming[nashville.ID])
	require.Equal(t, 1, api.finds, "the name is only looked up once")
}

func TestLookahead(t *testing.T) {
	t.Parallel()

	b, _ := testBoard(t, "9-1-1 (tvmaze 28152)")
	require.Len(t, b.upcoming(context.Background()), 1)

	b, _ = testBoard(t, "9-1-1 (tvmaze 28152)")
	b.config.LookaheadDays = 8
	require.Len(t, b.upcoming(context.Background()), 2)
}

func TestFailureKeepsWhatItHad(t *testing.T) {
	t.Parallel()

	b, api := testBoard(t, "9-1-1: Nashville (tvmaze 82759)")
	require.Len(t, b.upcoming(context.Background()), 1)

	api.fail = errors.New("down")
	b.now = func() time.Time { return testNow.Add(refreshEvery) }
	require.Len(t, b.upcoming(context.Background()), 1)
	require.Equal(t, 2, api.upcoming[nashville.ID])

	// and waits before asking again
	b.now = func() time.Time { return testNow.Add(refreshEvery + time.Minute) }
	b.upcoming(context.Background())
	require.Equal(t, 2, api.upcoming[nashville.ID])

	// after a day with nothing new, it gives up on the old
	b.now = func() time.Time { return testNow.Add(staleLimit + time.Hour) }
	require.Empty(t, b.upcoming(context.Background()))
}

func TestSetShows(t *testing.T) {
	t.Parallel()

	b, _ := testBoard(t, "Family Guy (tvmaze 84)")
	ctx := context.Background()

	saved, err := b.SetShows(ctx, []board.Show{{ID: 84, Name: "Family Guy"}, {ID: 82759, Name: "nashville"}, {ID: 84}})
	require.NoError(t, err)
	require.Equal(t, []string{"Family Guy (tvmaze 84)", "9-1-1: Nashville (tvmaze 82759)"}, saved, "TVmaze's name, once each")
	require.Equal(t, saved, b.config.Shows)

	shows := b.Shows(ctx)
	require.Len(t, shows, 2)
	require.Equal(t, board.Show{ID: 82759, Name: "9-1-1: Nashville", Network: "ABC", Premiered: "2025"}, shows[1])

	_, err = b.SetShows(ctx, []board.Show{{ID: 999999, Name: "Nope"}})
	require.ErrorIs(t, err, ErrBadShow)
	require.ErrorIs(t, err, tvmaze.ErrNotFound)
	require.Len(t, b.Shows(ctx), 2, "a refused list changes nothing")

	_, err = b.SetShows(ctx, []board.Show{{Name: "No ID"}})
	require.ErrorIs(t, err, ErrBadShow)

	saved, err = b.SetShows(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, saved)
}

func TestSearchShows(t *testing.T) {
	t.Parallel()

	b, _ := testBoard(t)
	shows, err := b.SearchShows(context.Background(), "911")
	require.NoError(t, err)
	require.Equal(t, "9-1-1", shows[0].Name)
	require.Equal(t, "ABC", shows[0].Network)

	_, err = b.SearchShows(context.Background(), "  ")
	require.ErrorIs(t, err, ErrBadShow)
}

func TestDayLabel(t *testing.T) {
	t.Parallel()

	at := func(d time.Duration, timed bool) tvmaze.Episode {
		return tvmaze.Episode{Airs: testNow.Add(d), Timed: timed}
	}
	require.Equal(t, "ON NOW", dayLabel(at(-10*time.Minute, true), testNow))
	require.Equal(t, "TODAY", dayLabel(at(8*time.Hour, true), testNow))
	require.Equal(t, "TODAY", dayLabel(at(-2*time.Hour, false), testNow), "a streaming drop isn't on now")
	require.Equal(t, "TOMORROW", dayLabel(at(20*time.Hour, true), testNow))
	require.Equal(t, "SAT OCT 17", dayLabel(at(50*time.Hour, true), testNow))
}

func TestClockLabel(t *testing.T) {
	t.Parallel()

	for in, want := range map[[2]int]string{{20, 0}: "8PM", {20, 30}: "8:30PM", {0, 0}: "12AM", {12, 5}: "12:05PM", {9, 0}: "9AM"} {
		require.Equal(t, want, clockLabel(time.Date(2026, 10, 15, in[0], in[1], 0, 0, time.UTC)))
	}
}

func TestWrap(t *testing.T) {
	t.Parallel()

	d, err := (&drawer{}).setup(image.Rect(0, 0, 64, 32))
	require.NoError(t, err)
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))

	require.Equal(t, []string{"FAMILY GUY"}, d.wrap(img, "FAMILY GUY", 64, 2))

	lines := d.wrap(img, "THE GREAT BRITISH BAKING SHOW: HOLIDAYS", 64, 2)
	require.Len(t, lines, 2)
	for _, l := range lines {
		require.LessOrEqual(t, d.width(img, l), 64, l)
	}
}

// TestPreview writes a few episode screens to $PREVIEW_DIR, when it is set,
// for a person to look at.
func TestPreview(t *testing.T) {
	t.Parallel()

	dir := os.Getenv("PREVIEW_DIR")
	if dir == "" {
		t.Skip("PREVIEW_DIR not set")
	}
	d, err := (&drawer{}).setup(image.Rect(0, 0, 64, 32))
	require.NoError(t, err)

	eps := []tvmaze.Episode{
		{Show: nashville, Season: 2, Number: 1, Airs: testNow.Add(8 * time.Hour), Timed: true},
		{Show: familyGuy, Season: 25, Number: 7, Airs: testNow.Add(31 * time.Hour), Timed: true},
		{Show: tvmaze.Show{Name: "American Dad!", Network: "FOX"}, Season: 21, Number: 12, Airs: testNow.Add(56*time.Hour + 30*time.Minute), Timed: true},
		{Show: tvmaze.Show{Name: "The Great British Baking Show: Holidays", Network: "Netflix"}, Season: 8, Number: 0, Special: true, Airs: testNow.Add(3 * 24 * time.Hour)},
		{Show: survivor, Season: 49, Number: 4, Airs: testNow.Add(-10 * time.Minute), Timed: true},
	}
	for i, ep := range eps {
		img := image.NewRGBA(image.Rect(0, 0, 64, 32))
		require.NoError(t, d.episode(img, ep, testNow))
		f, err := os.Create(fmt.Sprintf("%s/tv_%d.png", dir, i))
		require.NoError(t, err)
		require.NoError(t, png.Encode(f, img))
		require.NoError(t, f.Close())
	}
}
