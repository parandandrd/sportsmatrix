package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// NWS gets weather from the US National Weather Service, api.weather.gov. It
// needs no key, but only covers the US and its territories.
//
// A forecast takes a lookup first: the Weather Service turns a point into the
// forecast office and grid square that cover it, and names the stations
// nearest to it. Those never change for a point, so NWS keeps them.
type NWS struct {
	client *http.Client
	base   string
	now    func() time.Time

	lock   sync.Mutex
	points map[Location]*nwsPoint
}

type nwsPoint struct {
	forecast, hourly, stations string
	station                    string
	place                      string
}

// NewNWS returns a Weather Service provider. base is the API's address, and
// only tests need to give one.
func NewNWS(client *http.Client, base string) *NWS {
	if base == "" {
		base = "https://api.weather.gov"
	}
	return &NWS{
		client: client,
		base:   strings.TrimRight(base, "/"),
		now:    time.Now,
		points: make(map[Location]*nwsPoint),
	}
}

// ErrOutsideCoverage is a location the provider has no weather for.
var ErrOutsideCoverage = errors.New("no weather for that location")

type coverageError struct {
	msg string
}

func (e *coverageError) Error() string { return e.msg }
func (e *coverageError) Is(target error) bool {
	return target == ErrOutsideCoverage
}

type nwsError struct {
	status int
	detail string
}

func (e *nwsError) Error() string {
	if e.detail != "" {
		return fmt.Sprintf("the Weather Service answered %d: %s", e.status, e.detail)
	}
	return fmt.Sprintf("the Weather Service answered %d", e.status)
}

func (n *NWS) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/geo+json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		var problem struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(body, &problem)
		detail := problem.Detail
		if detail == "" {
			detail = problem.Title
		}
		return &nwsError{status: resp.StatusCode, detail: strings.TrimSpace(detail)}
	}

	return json.Unmarshal(body, out)
}

// point looks up, once, the grid square and station that cover loc.
func (n *NWS) point(ctx context.Context, loc Location) (*nwsPoint, error) {
	// The Weather Service takes four decimal places at most, and answers more
	// with a redirect.
	loc = Location{Lat: math.Round(loc.Lat*1e4) / 1e4, Lon: math.Round(loc.Lon*1e4) / 1e4}

	n.lock.Lock()
	p, ok := n.points[loc]
	n.lock.Unlock()
	if ok {
		return p, nil
	}

	var points struct {
		Properties struct {
			Forecast            string `json:"forecast"`
			ForecastHourly      string `json:"forecastHourly"`
			ObservationStations string `json:"observationStations"`
			RelativeLocation    struct {
				Properties struct {
					City  string `json:"city"`
					State string `json:"state"`
				} `json:"properties"`
			} `json:"relativeLocation"`
		} `json:"properties"`
	}
	u := fmt.Sprintf("%s/points/%s,%s", n.base, trimFloat(loc.Lat), trimFloat(loc.Lon))
	if err := n.get(ctx, u, &points); err != nil {
		var nerr *nwsError
		if errors.As(err, &nerr) && nerr.status == http.StatusNotFound {
			return nil, &coverageError{msg: "the Weather Service only covers the US; try the open-meteo provider"}
		}
		return nil, err
	}

	props := points.Properties
	if props.Forecast == "" || props.ForecastHourly == "" {
		return nil, &coverageError{msg: fmt.Sprintf("the Weather Service has no forecast for %s", loc)}
	}

	p = &nwsPoint{
		forecast: props.Forecast,
		hourly:   props.ForecastHourly,
		stations: props.ObservationStations,
	}
	if rel := props.RelativeLocation.Properties; rel.City != "" {
		p.place = rel.City
		if rel.State != "" {
			p.place += ", " + rel.State
		}
	}

	n.lock.Lock()
	n.points[loc] = p
	n.lock.Unlock()

	return p, nil
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.4f", f)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// Check looks up loc, which fails for a point the Weather Service doesn't cover.
func (n *NWS) Check(ctx context.Context, loc Location) error {
	_, err := n.point(ctx, loc)
	return err
}

type nwsPeriod struct {
	Name            string    `json:"name"`
	StartTime       time.Time `json:"startTime"`
	IsDaytime       bool      `json:"isDaytime"`
	Temperature     float64   `json:"temperature"`
	TemperatureUnit string    `json:"temperatureUnit"`
	PrecipChance    struct {
		Value *float64 `json:"value"`
	} `json:"probabilityOfPrecipitation"`
	ShortForecast string `json:"shortForecast"`
	Icon          string `json:"icon"`
}

type nwsForecast struct {
	Properties struct {
		Periods []nwsPeriod `json:"periods"`
	} `json:"properties"`
}

// Fetch gets the forecast, the hourly forecast and the latest observation for
// loc. The observation is only a nicety: when the nearest station has nothing
// recent, the current hour's forecast stands in for it.
func (n *NWS) Fetch(ctx context.Context, loc Location, units Units) (*Report, error) {
	p, err := n.point(ctx, loc)
	if err != nil {
		return nil, err
	}

	q := "?units=us"
	if units == Celsius {
		q = "?units=si"
	}

	var daily, hourly nwsForecast
	if err := n.get(ctx, p.forecast+q, &daily); err != nil {
		return nil, fmt.Errorf("forecast: %w", err)
	}
	if err := n.get(ctx, p.hourly+q, &hourly); err != nil {
		return nil, fmt.Errorf("hourly forecast: %w", err)
	}

	r := &Report{Place: p.place}
	for _, per := range hourly.Properties.Periods {
		r.Hours = append(r.Hours, per.conditions())
	}
	r.Days = nwsDays(daily.Properties.Periods)

	now := n.now()
	for _, h := range r.Hours {
		if h.Start.After(now) {
			break
		}
		r.Now = h
	}
	if r.Now.Start.IsZero() && len(r.Hours) > 0 {
		r.Now = r.Hours[0]
	}

	if obs, err := n.observation(ctx, p, units); err == nil && now.Sub(obs.Start) < 2*time.Hour {
		if obs.Kind == Unknown {
			obs.Kind, obs.Day = r.Now.Kind, r.Now.Day
		}
		if obs.Text == "" {
			obs.Text = r.Now.Text
		}
		obs.PrecipChance = r.Now.PrecipChance
		r.Now = obs
	}

	// The hourly forecast begins with the hour already under way.
	for len(r.Hours) > 0 && !r.Hours[0].Start.After(now) {
		r.Hours = r.Hours[1:]
	}

	return r, nil
}

func (per nwsPeriod) conditions() Conditions {
	kind, day := nwsIcon(per.Icon)
	if per.Icon == "" {
		day = per.IsDaytime
	}
	c := Conditions{
		Temp:         per.Temperature,
		Kind:         kind,
		Day:          day,
		Text:         per.ShortForecast,
		Start:        per.StartTime,
		PrecipChance: -1,
	}
	if per.PrecipChance.Value != nil {
		c.PrecipChance = int(math.Round(*per.PrecipChance.Value))
	}
	return c
}

// nwsDays pairs each day's daytime period with the night that follows it. The
// first period can be a night ("Tonight"), and then today has only a low.
func nwsDays(periods []nwsPeriod) []Day {
	var days []Day
	for _, per := range periods {
		y, m, d := per.StartTime.Date()
		date := time.Date(y, m, d, 0, 0, 0, 0, per.StartTime.Location())

		if len(days) == 0 || !days[len(days)-1].Date.Equal(date) {
			days = append(days, Day{Date: date, PrecipChance: -1})
		}
		day := &days[len(days)-1]

		c := per.conditions()
		if per.IsDaytime {
			day.High, day.HasHigh = per.Temperature, true
			day.Kind, day.Text = c.Kind, c.Text
		} else {
			day.Low, day.HasLow = per.Temperature, true
			if !day.HasHigh {
				day.Kind, day.Text = c.Kind, c.Text
			}
		}
		if c.PrecipChance > day.PrecipChance {
			day.PrecipChance = c.PrecipChance
		}
	}
	return days
}

func (n *NWS) observation(ctx context.Context, p *nwsPoint, units Units) (Conditions, error) {
	if p.stations == "" {
		return Conditions{}, errors.New("no observation stations")
	}

	n.lock.Lock()
	station := p.station
	n.lock.Unlock()

	if station == "" {
		var stations struct {
			Features []struct {
				Properties struct {
					StationIdentifier string `json:"stationIdentifier"`
				} `json:"properties"`
			} `json:"features"`
		}
		if err := n.get(ctx, p.stations+"?limit=1", &stations); err != nil {
			return Conditions{}, err
		}
		if len(stations.Features) == 0 {
			return Conditions{}, errors.New("no observation stations")
		}
		station = stations.Features[0].Properties.StationIdentifier

		n.lock.Lock()
		p.station = station
		n.lock.Unlock()
	}

	var obs struct {
		Properties struct {
			Timestamp       time.Time `json:"timestamp"`
			TextDescription string    `json:"textDescription"`
			Icon            *string   `json:"icon"`
			Temperature     struct {
				Value    *float64 `json:"value"`
				UnitCode string   `json:"unitCode"`
			} `json:"temperature"`
		} `json:"properties"`
	}
	u := fmt.Sprintf("%s/stations/%s/observations/latest", n.base, url.PathEscape(station))
	if err := n.get(ctx, u, &obs); err != nil {
		return Conditions{}, err
	}

	props := obs.Properties
	if props.Temperature.Value == nil {
		return Conditions{}, errors.New("the latest observation has no temperature")
	}

	temp := *props.Temperature.Value
	if strings.HasSuffix(props.Temperature.UnitCode, "degF") {
		if units == Celsius {
			temp = (temp - 32) * 5 / 9
		}
	} else if units == Fahrenheit {
		temp = fahrenheit(temp)
	}

	c := Conditions{
		Temp:         temp,
		Text:         props.TextDescription,
		Start:        props.Timestamp,
		PrecipChance: -1,
	}
	if props.Icon != nil {
		c.Kind, c.Day = nwsIcon(*props.Icon)
	}
	return c, nil
}

// nwsIcon reads what an icon shows from its address, as in
// https://api.weather.gov/icons/land/night/rain_showers,40/tsra,40?size=small:
// night, and rain showers turning to thunderstorms. The first condition is the
// one that holds now.
func nwsIcon(icon string) (Kind, bool) {
	u, err := url.Parse(icon)
	if err != nil {
		return Unknown, true
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, part := range parts {
		if (part == "day" || part == "night") && i+1 < len(parts) {
			code, _, _ := strings.Cut(parts[i+1], ",")
			return nwsKinds[code], part == "day"
		}
	}

	return Unknown, true
}

// nwsKinds are the Weather Service's icon codes, from api.weather.gov/icons.
var nwsKinds = map[string]Kind{
	"skc":             Clear,
	"few":             Clear,
	"sct":             PartlyCloudy,
	"bkn":             PartlyCloudy,
	"ovc":             Cloudy,
	"wind_skc":        Wind,
	"wind_few":        Wind,
	"wind_sct":        Wind,
	"wind_bkn":        Wind,
	"wind_ovc":        Wind,
	"snow":            Snow,
	"blizzard":        Snow,
	"rain_snow":       Sleet,
	"rain_sleet":      Sleet,
	"snow_sleet":      Sleet,
	"fzra":            Sleet,
	"rain_fzra":       Sleet,
	"snow_fzra":       Sleet,
	"sleet":           Sleet,
	"rain":            Rain,
	"rain_showers":    Rain,
	"rain_showers_hi": Rain,
	"tsra":            Thunder,
	"tsra_sct":        Thunder,
	"tsra_hi":         Thunder,
	"tornado":         Thunder,
	"hurricane":       Thunder,
	"tropical_storm":  Thunder,
	"dust":            Fog,
	"smoke":           Fog,
	"haze":            Fog,
	"fog":             Fog,
	"hot":             Clear,
	"cold":            Clear,
}
