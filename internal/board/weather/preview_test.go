package weatherboard

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/parandandrd/sportsmatrix/internal/weather"
)

// TestPreviewIcons writes every icon to $PREVIEW_DIR, when it is set, for a
// person to look at.
func TestPreviewIcons(t *testing.T) {
	dir := os.Getenv("PREVIEW_DIR")
	if dir == "" {
		t.Skip("PREVIEW_DIR not set")
	}
	kinds := []weather.Kind{weather.Clear, weather.Clear, weather.PartlyCloudy, weather.PartlyCloudy, weather.Cloudy,
		weather.Rain, weather.Thunder, weather.Snow, weather.Sleet, weather.Fog, weather.Wind}
	img := image.NewRGBA(image.Rect(0, 0, len(kinds)*(iconSize+2), iconSize))
	for i, k := range kinds {
		drawIcon(img, image.Pt(i*(iconSize+2), 0), iconFor(k, i%2 == 0 || i > 3), 1)
	}
	f, err := os.Create(dir + "/icons.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// sampleReport is a report with a bit of everything in it.
func sampleReport(now time.Time) *weather.Report {
	r := &weather.Report{
		Now: weather.Conditions{Temp: 62.6, Kind: weather.Cloudy, Day: true, Text: "Isolated Rain Showers then Mostly Cloudy", Start: now},
	}
	kinds := []weather.Kind{weather.Cloudy, weather.PartlyCloudy, weather.Rain, weather.Thunder, weather.Clear, weather.Clear, weather.PartlyCloudy, weather.Fog, weather.Snow}
	for i := 0; i < 24; i++ {
		t := now.Truncate(time.Hour).Add(time.Duration(i+1) * time.Hour)
		r.Hours = append(r.Hours, weather.Conditions{
			Temp: 62 + float64(i%5), Kind: kinds[i%len(kinds)], Day: t.Hour() >= 7 && t.Hour() < 19, Start: t, PrecipChance: (i * 13) % 70,
		})
	}
	days := []weather.Day{
		{High: 65, Low: 56, Kind: weather.Rain, PrecipChance: 10},
		{High: 66, Low: 56, Kind: weather.Clear, PrecipChance: 0},
		{High: 71, Low: 58, Kind: weather.PartlyCloudy, PrecipChance: 20},
		{High: 104, Low: -12, Kind: weather.Thunder, PrecipChance: 60},
		{High: 48, Low: 31, Kind: weather.Snow, PrecipChance: 80},
		{High: 55, Low: 40, Kind: weather.Sleet},
	}
	for i := range days {
		days[i].HasHigh, days[i].HasLow = true, true
		days[i].Date = time.Date(now.Year(), now.Month(), now.Day()+i, 0, 0, 0, 0, now.Location())
	}
	r.Days = days
	return r
}

func writePNG(t *testing.T, name string, img image.Image) {
	t.Helper()
	f, err := os.Create(os.Getenv("PREVIEW_DIR") + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// TestPreviewScreens writes each screen, at 64x32 and 128x64, to $PREVIEW_DIR.
func TestPreviewScreens(t *testing.T) {
	if os.Getenv("PREVIEW_DIR") == "" {
		t.Skip("PREVIEW_DIR not set")
	}
	now := time.Date(2026, 9, 25, 9, 48, 0, 0, time.Local)
	r := sampleReport(now)

	for _, size := range []image.Point{{64, 32}, {128, 64}} {
		d, err := (&drawer{}).setup(image.Rect(0, 0, size.X, size.Y))
		if err != nil {
			t.Fatal(err)
		}
		bounds := image.Rect(0, 0, size.X, size.Y)
		n := columns(bounds, d.scale)

		img := image.NewRGBA(bounds)
		if err := d.now(img, r, now); err != nil {
			t.Fatal(err)
		}
		writePNG(t, fmt.Sprintf("now_%d.png", size.X), img)

		img = image.NewRGBA(bounds)
		if err := d.hours(img, pickHours(r.Hours, n), n); err != nil {
			t.Fatal(err)
		}
		writePNG(t, fmt.Sprintf("hours_%d.png", size.X), img)

		days := upcomingDays(r.Days, now, 6)
		for page := 0; page*n < len(days); page++ {
			img = image.NewRGBA(bounds)
			if err := d.days(img, days[page*n:min((page+1)*n, len(days))], n); err != nil {
				t.Fatal(err)
			}
			writePNG(t, fmt.Sprintf("days%d_%d.png", page, size.X), img)
		}
	}
}
