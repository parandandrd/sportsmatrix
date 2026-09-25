// Package weather fetches current conditions and forecasts from the US
// National Weather Service or Open-Meteo, and turns either into one Report the
// weather boards draw from.
package weather

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Kind is what a condition looks like, which is all an icon needs to know.
type Kind int

const (
	Unknown Kind = iota
	Clear
	PartlyCloudy
	Cloudy
	Rain
	Thunder
	Snow
	Sleet
	Fog
	Wind
)

var kindNames = []string{"unknown", "clear", "partly cloudy", "cloudy", "rain", "thunder", "snow", "sleet", "fog", "wind"}

func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Conditions are the weather at one time, or over one period.
type Conditions struct {
	Temp  float64
	Kind  Kind
	Day   bool
	Text  string
	Start time.Time
	// PrecipChance is the chance of precipitation in percent, or -1 when the
	// provider didn't give one.
	PrecipChance int
}

// Day is one day of a forecast. A day already partly over can be missing its
// high; HasHigh and HasLow say which it has.
type Day struct {
	Date         time.Time
	High, Low    float64
	HasHigh      bool
	HasLow       bool
	Kind         Kind
	Text         string
	PrecipChance int
}

// Report is everything the weather boards show.
type Report struct {
	Now   Conditions
	Hours []Conditions
	// Days starts with today.
	Days    []Day
	Fetched time.Time
	// Place is where the report is for, as the provider names it. It can be
	// empty.
	Place string
}

// Units say whether temperatures are in Fahrenheit or Celsius.
type Units int

const (
	Fahrenheit Units = iota
	Celsius
)

// ParseUnits reads the units setting: "imperial" or "fahrenheit" (the
// default), "metric" or "celsius".
func ParseUnits(s string) (Units, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "imperial", "fahrenheit", "f", "us":
		return Fahrenheit, nil
	case "metric", "celsius", "c", "si":
		return Celsius, nil
	}
	return Fahrenheit, badSetting("units %q, which can be imperial or metric", s)
}

// Location is a point, in decimal degrees.
type Location struct {
	Lat, Lon float64
}

func (l Location) String() string {
	return strconv.FormatFloat(l.Lat, 'f', 4, 64) + ", " + strconv.FormatFloat(l.Lon, 'f', 4, 64)
}

// ErrBadSetting is a location, provider or units setting that can't be used.
// errors.Is matches it, but it isn't part of the message: those are written
// to be shown to whoever typed the setting.
var ErrBadSetting = errors.New("bad weather setting")

type settingError struct {
	err error
}

func (e *settingError) Error() string   { return e.err.Error() }
func (e *settingError) Unwrap() []error { return []error{ErrBadSetting, e.err} }

// SettingError marks err as a setting that can't be used, keeping its message.
func SettingError(err error) error {
	return &settingError{err: err}
}

func badSetting(format string, args ...any) error {
	return SettingError(fmt.Errorf(format, args...))
}

// ErrNoLocation is returned by a Source that hasn't been given a location.
var ErrNoLocation = errors.New("no weather location set")

// ParseLocation reads coordinates as Google Maps copies them, "41.8858,
// -87.6181": latitude, then longitude. Spaces and a missing comma are fine.
func ParseLocation(s string) (Location, error) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(fields) != 2 {
		return Location{}, badSetting("%q is not a latitude and longitude, like 41.8858, -87.6181", s)
	}

	lat, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || lat < -90 || lat > 90 {
		return Location{}, badSetting("latitude %s, which goes from -90 to 90", fields[0])
	}
	lon, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || lon < -180 || lon > 180 {
		return Location{}, badSetting("longitude %s, which goes from -180 to 180", fields[1])
	}

	return Location{Lat: lat, Lon: lon}, nil
}

// Provider fetches a report for a location.
type Provider interface {
	Fetch(ctx context.Context, loc Location, units Units) (*Report, error)
	// Check says whether the provider can give a report for loc, without
	// fetching one. The Weather Service only covers the US.
	Check(ctx context.Context, loc Location) error
}

// ProviderNames are the providers NewProvider knows.
var ProviderNames = []string{"nws", "open-meteo"}

// NewProvider returns the provider called name: "nws" (the default) or
// "open-meteo".
func NewProvider(name string, client *http.Client) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "nws":
		return NewNWS(client, ""), nil
	case "open-meteo", "openmeteo":
		return NewOpenMeteo(client, ""), nil
	}
	return nil, badSetting("provider %q, which can be one of %s", name, strings.Join(ProviderNames, ", "))
}

// userAgent is what the Weather Service asks every client to send, so it can
// get in touch about a misbehaving one.
const userAgent = "sportsmatrix (github.com/parandandrd/sportsmatrix)"

// NewClient is an HTTP client with a timeout suited to the weather APIs.
func NewClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second}
}

// DefaultMaxAge is how old a report can get before a Source fetches another.
const DefaultMaxAge = 10 * time.Minute

// staleLimit is how old a report can be and still be shown when fetching a
// new one fails.
const staleLimit = 3 * time.Hour

// Source keeps the latest report for one location, fetching a new one when
// it is asked for a report older than MaxAge. It is shared by every weather
// board, so they show the same weather and fetch it once between them.
type Source struct {
	provider Provider
	units    Units
	log      *zap.Logger
	now      func() time.Time
	MaxAge   time.Duration

	lock     sync.Mutex
	loc      *Location
	report   *Report
	lastFail time.Time
}

// NewSource returns a source for loc, or for nowhere yet when loc is nil.
func NewSource(provider Provider, units Units, loc *Location, logger *zap.Logger) *Source {
	return &Source{
		provider: provider,
		units:    units,
		loc:      loc,
		log:      logger,
		now:      time.Now,
		MaxAge:   DefaultMaxAge,
	}
}

// Location is where the source reports for, and false when it has no location.
func (s *Source) Location() (Location, bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.loc == nil {
		return Location{}, false
	}
	return *s.loc, true
}

// SetLocation checks that the provider can report for loc, then moves the
// source there. The report for the old location is dropped.
func (s *Source) SetLocation(ctx context.Context, loc Location) error {
	if err := s.provider.Check(ctx, loc); err != nil {
		return err
	}

	s.lock.Lock()
	defer s.lock.Unlock()
	s.loc = &loc
	s.report = nil
	s.lastFail = time.Time{}
	return nil
}

// failRetry is how long a source waits after a failed fetch before trying
// again, so that a board shown every minute doesn't hit a service that is down
// every minute.
const failRetry = 2 * time.Minute

// Report returns the latest report, fetching a new one first if the one it has
// is older than MaxAge. When that fails it falls back on the old report while
// it is less than a few hours old.
func (s *Source) Report(ctx context.Context) (*Report, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.loc == nil {
		return nil, ErrNoLocation
	}

	now := s.now()
	if s.report != nil && now.Sub(s.report.Fetched) < s.MaxAge {
		return s.report, nil
	}

	if !s.lastFail.IsZero() && now.Sub(s.lastFail) < failRetry {
		return s.stale(now, errors.New("waiting to retry the last failed fetch"))
	}

	r, err := s.provider.Fetch(ctx, *s.loc, s.units)
	if err != nil {
		s.lastFail = now
		s.log.Error("failed to fetch weather", zap.Stringer("location", s.loc), zap.Error(err))
		return s.stale(now, err)
	}

	r.Fetched = now
	s.report = r
	s.lastFail = time.Time{}
	return r, nil
}

func (s *Source) stale(now time.Time, err error) (*Report, error) {
	if s.report != nil && now.Sub(s.report.Fetched) < staleLimit {
		return s.report, nil
	}
	return nil, err
}

func fahrenheit(c float64) float64 {
	return c*9/5 + 32
}
