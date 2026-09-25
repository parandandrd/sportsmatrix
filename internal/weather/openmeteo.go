package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OpenMeteo gets weather from Open-Meteo, api.open-meteo.com. It needs no key
// and covers the whole world.
type OpenMeteo struct {
	client *http.Client
	base   string
	now    func() time.Time
}

// NewOpenMeteo returns an Open-Meteo provider. base is the API's address, and
// only tests need to give one.
func NewOpenMeteo(client *http.Client, base string) *OpenMeteo {
	if base == "" {
		base = "https://api.open-meteo.com"
	}
	return &OpenMeteo{client: client, base: strings.TrimRight(base, "/"), now: time.Now}
}

// Check accepts every location: Open-Meteo has a forecast for anywhere.
func (o *OpenMeteo) Check(ctx context.Context, loc Location) error {
	return nil
}

type openMeteoResponse struct {
	UTCOffsetSeconds int `json:"utc_offset_seconds"`
	Current          struct {
		Time        string  `json:"time"`
		Temperature float64 `json:"temperature_2m"`
		WeatherCode int     `json:"weather_code"`
		IsDay       int     `json:"is_day"`
	} `json:"current"`
	Hourly struct {
		Time         []string   `json:"time"`
		Temperature  []float64  `json:"temperature_2m"`
		WeatherCode  []int      `json:"weather_code"`
		IsDay        []int      `json:"is_day"`
		PrecipChance []*float64 `json:"precipitation_probability"`
	} `json:"hourly"`
	Daily struct {
		Time         []string   `json:"time"`
		High         []*float64 `json:"temperature_2m_max"`
		Low          []*float64 `json:"temperature_2m_min"`
		WeatherCode  []int      `json:"weather_code"`
		PrecipChance []*float64 `json:"precipitation_probability_max"`
	} `json:"daily"`
	Reason string `json:"reason"`
}

// Fetch gets current conditions, the next day's hours and a week of days.
func (o *OpenMeteo) Fetch(ctx context.Context, loc Location, units Units) (*Report, error) {
	q := url.Values{}
	q.Set("latitude", trimFloat(loc.Lat))
	q.Set("longitude", trimFloat(loc.Lon))
	q.Set("current", "temperature_2m,weather_code,is_day")
	q.Set("hourly", "temperature_2m,weather_code,is_day,precipitation_probability")
	q.Set("daily", "temperature_2m_max,temperature_2m_min,weather_code,precipitation_probability_max")
	q.Set("timezone", "auto")
	q.Set("forecast_days", "7")
	q.Set("forecast_hours", "25")
	q.Set("temperature_unit", "fahrenheit")
	if units == Celsius {
		q.Set("temperature_unit", "celsius")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.base+"/v1/forecast?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	var om openMeteoResponse
	if err := json.Unmarshal(body, &om); err != nil {
		return nil, fmt.Errorf("Open-Meteo answered %d with something unreadable: %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Open-Meteo answered %d: %s", resp.StatusCode, om.Reason)
	}

	return om.report(o.now())
}

func (om *openMeteoResponse) report(now time.Time) (*Report, error) {
	zone := time.FixedZone("", om.UTCOffsetSeconds)
	parse := func(s string) (time.Time, error) {
		if len(s) == len("2006-01-02") {
			return time.ParseInLocation("2006-01-02", s, zone)
		}
		return time.ParseInLocation("2006-01-02T15:04", s, zone)
	}

	r := &Report{}

	start, err := parse(om.Current.Time)
	if err != nil {
		return nil, fmt.Errorf("Open-Meteo's current time: %w", err)
	}
	r.Now = Conditions{
		Temp:         om.Current.Temperature,
		Kind:         wmoKind(om.Current.WeatherCode),
		Day:          om.Current.IsDay == 1,
		Text:         wmoText(om.Current.WeatherCode),
		Start:        start,
		PrecipChance: -1,
	}

	h := om.Hourly
	for i, ts := range h.Time {
		if i >= len(h.Temperature) || i >= len(h.WeatherCode) {
			break
		}
		t, err := parse(ts)
		if err != nil {
			return nil, fmt.Errorf("Open-Meteo's hourly time: %w", err)
		}
		if !t.After(now) {
			// the hour under way is what Now already says
			continue
		}
		c := Conditions{
			Temp:         h.Temperature[i],
			Kind:         wmoKind(h.WeatherCode[i]),
			Day:          i < len(h.IsDay) && h.IsDay[i] == 1,
			Text:         wmoText(h.WeatherCode[i]),
			Start:        t,
			PrecipChance: percent(h.PrecipChance, i),
		}
		r.Hours = append(r.Hours, c)
		if r.Now.PrecipChance < 0 {
			r.Now.PrecipChance = c.PrecipChance
		}
	}

	d := om.Daily
	for i, ds := range d.Time {
		date, err := parse(ds)
		if err != nil {
			return nil, fmt.Errorf("Open-Meteo's daily date: %w", err)
		}
		day := Day{Date: date, PrecipChance: percent(d.PrecipChance, i)}
		if i < len(d.High) && d.High[i] != nil {
			day.High, day.HasHigh = *d.High[i], true
		}
		if i < len(d.Low) && d.Low[i] != nil {
			day.Low, day.HasLow = *d.Low[i], true
		}
		if i < len(d.WeatherCode) {
			day.Kind, day.Text = wmoKind(d.WeatherCode[i]), wmoText(d.WeatherCode[i])
		}
		r.Days = append(r.Days, day)
	}

	return r, nil
}

func percent(values []*float64, i int) int {
	if i >= len(values) || values[i] == nil {
		return -1
	}
	return int(math.Round(*values[i]))
}

// wmoKind reads a WMO weather interpretation code, as Open-Meteo gives them.
func wmoKind(code int) Kind {
	switch {
	case code <= 1:
		return Clear
	case code == 2:
		return PartlyCloudy
	case code == 3:
		return Cloudy
	case code == 45 || code == 48:
		return Fog
	case code == 56 || code == 57 || code == 66 || code == 67:
		return Sleet
	case code >= 51 && code <= 65, code >= 80 && code <= 82:
		return Rain
	case code >= 71 && code <= 77, code == 85 || code == 86:
		return Snow
	case code >= 95:
		return Thunder
	}
	return Unknown
}

var wmoTexts = map[int]string{
	0:  "Clear",
	1:  "Mostly Clear",
	2:  "Partly Cloudy",
	3:  "Cloudy",
	45: "Fog",
	48: "Freezing Fog",
	51: "Light Drizzle",
	53: "Drizzle",
	55: "Heavy Drizzle",
	56: "Freezing Drizzle",
	57: "Freezing Drizzle",
	61: "Light Rain",
	63: "Rain",
	65: "Heavy Rain",
	66: "Freezing Rain",
	67: "Freezing Rain",
	71: "Light Snow",
	73: "Snow",
	75: "Heavy Snow",
	77: "Snow Grains",
	80: "Showers",
	81: "Showers",
	82: "Heavy Showers",
	85: "Snow Showers",
	86: "Snow Showers",
	95: "Thunderstorms",
	96: "Thunderstorms",
	99: "Thunderstorms",
}

func wmoText(code int) string {
	return wmoTexts[code]
}
