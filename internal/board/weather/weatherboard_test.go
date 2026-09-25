package weatherboard

import (
	"context"
	"errors"
	"image"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/weather"
)

type fakeProvider struct {
	lock    sync.Mutex
	fetches int
	fail    error
}

func (f *fakeProvider) Fetch(ctx context.Context, loc weather.Location, units weather.Units) (*weather.Report, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.fetches++
	if f.fail != nil {
		return nil, f.fail
	}
	return sampleReport(time.Now()), nil
}

func (f *fakeProvider) Check(ctx context.Context, loc weather.Location) error {
	if loc.Lat < 20 {
		return weather.ErrOutsideCoverage
	}
	return nil
}

// frameCanvas keeps a copy of each frame it is asked to show.
type frameCanvas struct {
	*board.BlankCanvas
	lock   sync.Mutex
	frames []*image.RGBA
}

func newFrameCanvas() *frameCanvas {
	return &frameCanvas{BlankCanvas: board.NewBlankCanvas(64, 32, zap.NewNop())}
}

func (c *frameCanvas) Render(ctx context.Context) error {
	img := image.NewRGBA(c.Bounds())
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			img.Set(x, y, c.At(x, y))
		}
	}
	c.lock.Lock()
	c.frames = append(c.frames, img)
	c.lock.Unlock()
	return c.BlankCanvas.Clear()
}

func (c *frameCanvas) lit(i int) int {
	c.lock.Lock()
	defer c.lock.Unlock()
	n := 0
	f := c.frames[i]
	for y := 0; y < f.Bounds().Dy(); y++ {
		for x := 0; x < f.Bounds().Dx(); x++ {
			if r, g, b, _ := f.At(x, y).RGBA(); r|g|b != 0 {
				n++
			}
		}
	}
	return n
}

func testBoards(t *testing.T, location string) (*CurrentBoard, *ForecastBoard, *fakeProvider) {
	t.Helper()
	cfg := &Config{
		StartEnabled: atomic.NewBool(true),
		Location:     location,
		Forecast:     &ForecastConfig{StartEnabled: atomic.NewBool(true)},
	}
	cfg.SetDefaults()

	p := &fakeProvider{}
	current, forecast, err := newBoards(cfg, p, zap.NewNop())
	require.NoError(t, err)
	current.SetBoardDelay(time.Millisecond)
	forecast.SetBoardDelay(time.Millisecond)
	return current, forecast, p
}

func TestDefaults(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	cfg.SetDefaults()
	require.False(t, cfg.StartEnabled.Load())
	require.Equal(t, defaultDelay, cfg.boardDelay.Load())
	require.NotNil(t, cfg.Forecast)
	require.Equal(t, defaultDays, cfg.Forecast.Days)

	cfg = &Config{Forecast: &ForecastConfig{Days: 40, BoardDelay: "20s"}}
	cfg.SetDefaults()
	require.Equal(t, maxDays, cfg.Forecast.Days)
	require.Equal(t, 20*time.Second, cfg.Forecast.boardDelay.Load())
}

func TestBoardsShareAService(t *testing.T) {
	t.Parallel()

	current, forecast, _ := testBoards(t, "")
	require.Equal(t, CurrentName, current.Name())
	require.Equal(t, ForecastName, forecast.Name())

	path, h := current.GetRPCHandler()
	require.NotNil(t, h)
	require.Equal(t, "/weather/board.v1.BasicBoard/", path)
	path, _ = forecast.GetRPCHandler()
	require.Equal(t, "/forecast/weather/board.v1.BasicBoard/", path)
}

func TestNoLocationSkips(t *testing.T) {
	t.Parallel()

	current, forecast, p := testBoards(t, "")
	canvas := newFrameCanvas()

	require.NoError(t, current.Render(context.Background(), canvas))
	require.NoError(t, forecast.Render(context.Background(), canvas))
	require.Empty(t, canvas.frames)
	require.Zero(t, p.fetches)

	loc, place := current.Location(context.Background())
	require.Empty(t, loc)
	require.Empty(t, place)
}

func TestBadLocationInConfigSkips(t *testing.T) {
	t.Parallel()

	current, _, _ := testBoards(t, "somewhere nice")
	canvas := newFrameCanvas()
	require.NoError(t, current.Render(context.Background(), canvas))
	require.Empty(t, canvas.frames)
}

func TestCurrentShowsNowThenHours(t *testing.T) {
	t.Parallel()

	current, forecast, p := testBoards(t, "41.8858, -87.6181")
	canvas := newFrameCanvas()

	require.NoError(t, current.Render(context.Background(), canvas))
	require.Len(t, canvas.frames, 2)
	require.Positive(t, canvas.lit(0))
	require.Positive(t, canvas.lit(1))

	// the big icon is in the top left of the first screen
	r, g, b, _ := canvas.frames[0].At(10, 12).RGBA()
	require.NotZero(t, r|g|b)

	// the forecast board uses the report the current conditions board fetched
	require.NoError(t, forecast.Render(context.Background(), canvas))
	require.Len(t, canvas.frames, 3)
	require.Equal(t, 1, p.fetches)
}

func TestForecastPages(t *testing.T) {
	t.Parallel()

	_, forecast, _ := testBoards(t, "41.8858, -87.6181")
	canvas := newFrameCanvas()

	require.NoError(t, forecast.Render(context.Background(), canvas))
	require.Len(t, canvas.frames, 1, "three days fit one screen")

	forecast.config.Days = 5
	canvas = newFrameCanvas()
	require.NoError(t, forecast.Render(context.Background(), canvas))
	require.Len(t, canvas.frames, 2, "five days take two screens")
}

func TestFetchFailureSkips(t *testing.T) {
	t.Parallel()

	current, _, p := testBoards(t, "41.8858, -87.6181")
	p.fail = errors.New("down")
	canvas := newFrameCanvas()
	require.NoError(t, current.Render(context.Background(), canvas))
	require.Empty(t, canvas.frames)
}

func TestDisabledSkips(t *testing.T) {
	t.Parallel()

	current, _, p := testBoards(t, "41.8858, -87.6181")
	current.Enabler().Disable()
	canvas := newFrameCanvas()
	require.NoError(t, current.Render(context.Background(), canvas))
	require.Empty(t, canvas.frames)
	require.Zero(t, p.fetches)
}

func TestRenderStopsWhenCanceled(t *testing.T) {
	t.Parallel()

	current, _, _ := testBoards(t, "41.8858, -87.6181")
	current.SetBoardDelay(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- current.Render(ctx, newFrameCanvas()) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Render kept going after being canceled")
	}
}

func TestSetLocation(t *testing.T) {
	t.Parallel()

	current, _, _ := testBoards(t, "")
	ctx := context.Background()

	saved, err := current.SetLocation(ctx, "41.88581234,-87.6181")
	require.NoError(t, err)
	require.Equal(t, "41.8858, -87.6181", saved)
	loc, _ := current.Location(ctx)
	require.Equal(t, "41.8858, -87.6181", loc)

	_, err = current.SetLocation(ctx, "not a place")
	require.ErrorIs(t, err, weather.ErrBadSetting)

	// the provider doesn't cover it
	_, err = current.SetLocation(ctx, "10, 10")
	require.ErrorIs(t, err, weather.ErrBadSetting)
	require.ErrorIs(t, err, weather.ErrOutsideCoverage)
	loc, _ = current.Location(ctx)
	require.Equal(t, "41.8858, -87.6181", loc, "a refused location leaves the old one")
}

func TestUpcomingDays(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 25, 22, 0, 0, 0, time.Local)
	r := sampleReport(now)
	days := upcomingDays(r.Days, now, 3)
	require.Len(t, days, 3)
	require.Equal(t, 26, days[0].Date.Day())
	require.Equal(t, 28, days[2].Date.Day())

	require.Len(t, upcomingDays(r.Days, now, 10), 5)
}

func TestPickHours(t *testing.T) {
	t.Parallel()

	r := sampleReport(time.Date(2026, 9, 25, 9, 48, 0, 0, time.Local))
	hours := pickHours(r.Hours, 3)
	require.Len(t, hours, 3)
	require.Equal(t, 10, hours[0].Start.Hour())
	require.Equal(t, 13, hours[1].Start.Hour())
	require.Equal(t, 16, hours[2].Start.Hour())

	require.Len(t, pickHours(r.Hours[:4], 3), 2)
	require.Empty(t, pickHours(nil, 3))
}

func TestHourLabel(t *testing.T) {
	t.Parallel()

	for h, want := range map[int]string{0: "12A", 9: "9A", 12: "12P", 13: "1P", 23: "11P"} {
		require.Equal(t, want, hourLabel(time.Date(2026, 9, 25, h, 0, 0, 0, time.Local)))
	}
}

func TestFitText(t *testing.T) {
	t.Parallel()

	d, err := (&drawer{}).setup(image.Rect(0, 0, 64, 32))
	require.NoError(t, err)
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))

	require.Equal(t, "Cloudy", d.fitText(img, "Cloudy", 64))
	require.Equal(t, "Rain Showers", d.fitText(img, "Isolated Rain Showers then Mostly Cloudy", 64))
	require.Equal(t, "Thunderstorms", d.fitText(img, "Chance Showers And Thunderstorms", 64))

	long := d.fitText(img, "Supercalifragilisticexpialidocious", 64)
	w, err := d.width(img, d.small, long)
	require.NoError(t, err)
	require.LessOrEqual(t, w, 64)
	require.NotEmpty(t, long)
}

func TestIconForEveryKind(t *testing.T) {
	t.Parallel()

	for k := weather.Unknown; k <= weather.Wind; k++ {
		for _, day := range []bool{true, false} {
			icon := iconFor(k, day)
			require.Len(t, icon, iconSize, k.String())
			for _, row := range icon {
				require.Len(t, row, iconSize, k.String())
				for i := 0; i < len(row); i++ {
					if row[i] != '.' {
						_, ok := palette[row[i]]
						require.True(t, ok, "%s uses %q, which has no color", k, row[i])
					}
				}
			}
		}
	}
}

func TestDegreeMarkIsDrawn(t *testing.T) {
	t.Parallel()

	d, err := (&drawer{}).setup(image.Rect(0, 0, 64, 32))
	require.NoError(t, err)
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	require.NoError(t, d.tempAt(img, d.small, image.Pt(0, 0), "62", 7, white))

	// the mark is a 2x2 square just after the number
	require.Equal(t, white, img.RGBAAt(7, 0))
	require.Equal(t, white, img.RGBAAt(8, 1))
}
