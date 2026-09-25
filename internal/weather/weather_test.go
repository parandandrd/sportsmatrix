package weather

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestParseLocation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in       string
		lat, lon float64
	}{
		{"41.8858, -87.6181", 41.8858, -87.6181},
		{"41.8858,-87.6181", 41.8858, -87.6181},
		{" 41.8858   -87.6181 ", 41.8858, -87.6181},
		{"-33.8688, 151.2093", -33.8688, 151.2093},
	} {
		loc, err := ParseLocation(tc.in)
		require.NoError(t, err, tc.in)
		require.Equal(t, Location{Lat: tc.lat, Lon: tc.lon}, loc, tc.in)
	}

	for _, bad := range []string{"", "41.8858", "Chicago, IL", "91, 10", "41, 181", "41, -87, 3"} {
		_, err := ParseLocation(bad)
		require.ErrorIs(t, err, ErrBadSetting, bad)
	}
}

func TestLocationString(t *testing.T) {
	t.Parallel()

	require.Equal(t, "41.8858, -87.6181", Location{Lat: 41.8858, Lon: -87.6181}.String())
}

func TestNWSIcon(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		icon string
		kind Kind
		day  bool
	}{
		{"https://api.weather.gov/icons/land/day/ovc?size=medium", Cloudy, true},
		{"https://api.weather.gov/icons/land/night/sct?size=small", PartlyCloudy, false},
		{"https://api.weather.gov/icons/land/night/rain_showers,40/tsra,40?size=small", Rain, false},
		{"https://api.weather.gov/icons/land/day/tsra_hi,20?size=medium", Thunder, true},
		{"https://api.weather.gov/icons/land/day/snow_fzra?size=medium", Sleet, true},
		{"https://api.weather.gov/icons/land/day/somethingnew?size=medium", Unknown, true},
		{"", Unknown, true},
	} {
		kind, day := nwsIcon(tc.icon)
		require.Equal(t, tc.kind, kind, tc.icon)
		require.Equal(t, tc.day, day, tc.icon)
	}
}

// fixtureNow is when the testdata was fetched.
var fixtureNow = time.Date(2026, 9, 25, 9, 48, 0, 0, time.FixedZone("CDT", -5*3600))

// nwsServer serves the Weather Service fixtures, with their links pointed back
// at itself. It counts the requests for each path.
func nwsServer(t *testing.T) (*httptest.Server, map[string]int) {
	t.Helper()

	var lock sync.Mutex
	hits := make(map[string]int)
	files := map[string]string{
		"/points/41.8858,-87.6181":              "nws_points.json",
		"/gridpoints/LOT/76,73/forecast":        "nws_forecast.json",
		"/gridpoints/LOT/76,73/forecast/hourly": "nws_hourly.json",
		"/gridpoints/LOT/76,73/stations":        "nws_stations.json",
		"/stations/KMDW/observations/latest":    "nws_observation.json",
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		lock.Lock()
		hits[req.URL.Path]++
		lock.Unlock()

		if req.Header.Get("User-Agent") != userAgent {
			http.Error(w, `{"title": "no user agent"}`, http.StatusForbidden)
			return
		}

		name, ok := files[req.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"title": "Data Unavailable For Requested Point", "detail": "Unable to provide data for requested point"}`))
			return
		}
		dat, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(string(dat), "https://api.weather.gov", srv.URL)))
	}))
	t.Cleanup(srv.Close)

	return srv, hits
}

func TestNWSFetch(t *testing.T) {
	t.Parallel()

	srv, hits := nwsServer(t)
	n := NewNWS(srv.Client(), srv.URL)
	n.now = func() time.Time { return fixtureNow }

	loc := Location{Lat: 41.8858, Lon: -87.6181}
	r, err := n.Fetch(context.Background(), loc, Fahrenheit)
	require.NoError(t, err)

	require.Equal(t, "Chicago, IL", r.Place)

	// Now is the station's observation, 17C at 9:30
	require.InDelta(t, 62.6, r.Now.Temp, 0.01)
	require.Equal(t, Cloudy, r.Now.Kind)
	require.True(t, r.Now.Day)
	require.Equal(t, "Cloudy", r.Now.Text)

	// the hours start after the one under way
	require.NotEmpty(t, r.Hours)
	require.Equal(t, 10, r.Hours[0].Start.Hour())
	require.Equal(t, float64(62), r.Hours[0].Temp)
	require.Equal(t, Cloudy, r.Hours[0].Kind)

	require.GreaterOrEqual(t, len(r.Days), 7)
	today := r.Days[0]
	require.Equal(t, 25, today.Date.Day())
	require.True(t, today.HasHigh)
	require.True(t, today.HasLow)
	require.Equal(t, float64(65), today.High)
	require.Equal(t, float64(56), today.Low)
	require.Equal(t, Rain, today.Kind)
	require.Equal(t, 10, today.PrecipChance)

	saturday := r.Days[1]
	require.Equal(t, time.Saturday, saturday.Date.Weekday())
	require.Equal(t, float64(66), saturday.High)
	require.Equal(t, Clear, saturday.Kind)
	require.Equal(t, "Sunny", saturday.Text)

	// a second fetch reuses the grid square and the station
	_, err = n.Fetch(context.Background(), loc, Fahrenheit)
	require.NoError(t, err)
	require.Equal(t, 1, hits["/points/41.8858,-87.6181"])
	require.Equal(t, 1, hits["/gridpoints/LOT/76,73/stations"])
	require.Equal(t, 2, hits["/gridpoints/LOT/76,73/forecast"])
}

func TestNWSOldObservation(t *testing.T) {
	t.Parallel()

	srv, _ := nwsServer(t)
	n := NewNWS(srv.Client(), srv.URL)
	// three hours on, the 9:30 observation is too old to be "now"
	n.now = func() time.Time { return fixtureNow.Add(3 * time.Hour) }

	r, err := n.Fetch(context.Background(), Location{Lat: 41.8858, Lon: -87.6181}, Fahrenheit)
	require.NoError(t, err)
	require.Equal(t, 12, r.Now.Start.Hour())
	require.Equal(t, float64(64), r.Now.Temp)
	require.Equal(t, 13, r.Hours[0].Start.Hour())
}

func TestNWSOutsideUS(t *testing.T) {
	t.Parallel()

	srv, _ := nwsServer(t)
	n := NewNWS(srv.Client(), srv.URL)

	err := n.Check(context.Background(), Location{Lat: 51.5072, Lon: -0.1276})
	require.ErrorIs(t, err, ErrOutsideCoverage)
	require.Contains(t, err.Error(), "only covers the US")
}

func TestOpenMeteoFetch(t *testing.T) {
	t.Parallel()

	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		query = req.URL.RawQuery
		http.ServeFile(w, req, "testdata/openmeteo.json")
	}))
	defer srv.Close()

	o := NewOpenMeteo(srv.Client(), srv.URL)
	o.now = func() time.Time { return fixtureNow }

	r, err := o.Fetch(context.Background(), Location{Lat: 41.8858, Lon: -87.6181}, Fahrenheit)
	require.NoError(t, err)
	require.Contains(t, query, "temperature_unit=fahrenheit")
	require.Contains(t, query, "latitude=41.8858")

	require.InDelta(t, 60.6, r.Now.Temp, 0.01)
	require.Equal(t, Cloudy, r.Now.Kind)
	require.Equal(t, "Cloudy", r.Now.Text)
	require.True(t, r.Now.Day)

	require.NotEmpty(t, r.Hours)
	require.Equal(t, 10, r.Hours[0].Start.Hour())
	require.True(t, fixtureNow.Add(12*time.Minute).Equal(r.Hours[0].Start))

	require.Len(t, r.Days, 7)
	require.Equal(t, 25, r.Days[0].Date.Day())
	require.True(t, r.Days[0].HasHigh)
	require.True(t, r.Days[0].HasLow)
	require.GreaterOrEqual(t, r.Days[0].High, r.Days[0].Low)
}

func TestWMOKind(t *testing.T) {
	t.Parallel()

	for code, kind := range map[int]Kind{
		0: Clear, 1: Clear, 2: PartlyCloudy, 3: Cloudy, 45: Fog, 53: Rain, 57: Sleet,
		63: Rain, 67: Sleet, 73: Snow, 81: Rain, 86: Snow, 95: Thunder, 99: Thunder, 42: Unknown,
	} {
		require.Equal(t, kind, wmoKind(code), code)
	}
}

type fakeProvider struct {
	lock    sync.Mutex
	fetches int
	fail    error
	temp    float64
}

func (f *fakeProvider) Fetch(ctx context.Context, loc Location, units Units) (*Report, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.fetches++
	if f.fail != nil {
		return nil, f.fail
	}
	return &Report{Now: Conditions{Temp: f.temp}}, nil
}

func (f *fakeProvider) Check(ctx context.Context, loc Location) error {
	if loc.Lat < 0 {
		return ErrOutsideCoverage
	}
	return nil
}

func TestSource(t *testing.T) {
	t.Parallel()

	p := &fakeProvider{temp: 60}
	s := NewSource(p, Fahrenheit, nil, zap.NewNop())
	now := fixtureNow
	s.now = func() time.Time { return now }
	ctx := context.Background()

	_, err := s.Report(ctx)
	require.ErrorIs(t, err, ErrNoLocation)

	require.ErrorIs(t, s.SetLocation(ctx, Location{Lat: -1}), ErrOutsideCoverage)
	_, ok := s.Location()
	require.False(t, ok)

	require.NoError(t, s.SetLocation(ctx, Location{Lat: 41.8858, Lon: -87.6181}))

	r, err := s.Report(ctx)
	require.NoError(t, err)
	require.Equal(t, float64(60), r.Now.Temp)
	require.Equal(t, now, r.Fetched)

	// fresh enough: no second fetch
	now = now.Add(5 * time.Minute)
	_, err = s.Report(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, p.fetches)

	// too old: fetched again
	now = now.Add(10 * time.Minute)
	p.temp = 61
	r, err = s.Report(ctx)
	require.NoError(t, err)
	require.Equal(t, float64(61), r.Now.Temp)
	require.Equal(t, 2, p.fetches)

	// a failure falls back on the last report, and waits before retrying
	now = now.Add(15 * time.Minute)
	p.fail = errors.New("down")
	r, err = s.Report(ctx)
	require.NoError(t, err)
	require.Equal(t, float64(61), r.Now.Temp)
	require.Equal(t, 3, p.fetches)

	now = now.Add(time.Minute)
	_, err = s.Report(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, p.fetches)

	// hours later the old report is no use
	now = now.Add(4 * time.Hour)
	_, err = s.Report(ctx)
	require.Error(t, err)
	require.Equal(t, 4, p.fetches)

	// a new location drops the old report
	p.fail = nil
	p.temp = 70
	require.NoError(t, s.SetLocation(ctx, Location{Lat: 40, Lon: -80}))
	r, err = s.Report(ctx)
	require.NoError(t, err)
	require.Equal(t, float64(70), r.Now.Temp)
}

func TestParseUnits(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]Units{"": Fahrenheit, "imperial": Fahrenheit, "Metric": Celsius, "celsius": Celsius} {
		got, err := ParseUnits(in)
		require.NoError(t, err)
		require.Equal(t, want, got, in)
	}
	_, err := ParseUnits("kelvin")
	require.ErrorIs(t, err, ErrBadSetting)
}

func TestNewProvider(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", "nws", "NWS", "open-meteo"} {
		_, err := NewProvider(name, NewClient())
		require.NoError(t, err, name)
	}
	_, err := NewProvider("accuweather", NewClient())
	require.ErrorIs(t, err, ErrBadSetting)
}
