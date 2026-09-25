package weatherboard

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/rgbrender"
	"github.com/parandandrd/sportsmatrix/internal/weather"
)

var (
	white     = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	dim       = color.RGBA{R: 190, G: 190, B: 190, A: 255}
	warm      = color.RGBA{R: 255, G: 150, B: 60, A: 255}
	cool      = color.RGBA{R: 110, G: 170, B: 255, A: 255}
	rainBlue  = color.RGBA{R: 60, G: 130, B: 255, A: 255}
	labelGold = color.RGBA{R: 255, G: 200, B: 80, A: 255}
)

// columnWidth is the least width one hour or one day gets on a 32-row panel.
const columnWidth = 21

// drawer holds the text writers for one board, made for the size of panel it
// first draws on.
type drawer struct {
	lock  sync.Mutex
	scale int
	small *rgbrender.TextWriter
	big   *rgbrender.TextWriter
}

// setup makes the writers for a panel bounds high, once.
func (d *drawer) setup(bounds image.Rectangle) (*drawer, error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	scale := bounds.Dy() / 32
	if scale < 1 {
		scale = 1
	}
	if d.small != nil && d.scale == scale {
		return d, nil
	}

	smallFont, err := rgbrender.GetFont("04B_03__.ttf")
	if err != nil {
		return nil, err
	}
	small := rgbrender.NewTextWriter(smallFont, 8*float64(scale))
	small.YStartCorrection = -2 * scale

	fnt, err := rgbrender.GetFont("score.ttf")
	if err != nil {
		return nil, err
	}
	big := rgbrender.NewTextWriter(fnt, 16*float64(scale))
	big.YStartCorrection = -3 * scale

	d.scale, d.small, d.big = scale, small, big
	return d, nil
}

// show draws each screen in turn onto the canvas, holding each for delay.
func show(ctx context.Context, canvas board.Canvas, delay func() time.Duration, screens []func(*image.RGBA) error) error {
	for _, screen := range screens {
		select {
		case <-ctx.Done():
			return context.Canceled
		default:
		}

		shown := time.Now()
		img := image.NewRGBA(canvas.Bounds())
		if err := screen(img); err != nil {
			return err
		}
		draw.Draw(canvas, canvas.Bounds(), img, canvas.Bounds().Min, draw.Over)
		if err := canvas.Render(ctx); err != nil {
			return err
		}

		if err := board.Hold(ctx, shown, delay()); err != nil {
			return err
		}
	}
	return nil
}

// Render shows the weather now, then the next few hours.
func (b *CurrentBoard) Render(ctx context.Context, canvas board.Canvas) error {
	if !b.enabler.Enabled() {
		return nil
	}

	r := b.report(ctx)
	if r == nil {
		return nil
	}

	d, err := b.draw.setup(canvas.Bounds())
	if err != nil {
		return err
	}

	now := time.Now()
	screens := []func(*image.RGBA) error{
		func(img *image.RGBA) error { return d.now(img, r, now) },
	}
	n := columns(canvas.Bounds(), d.scale)
	if hours := pickHours(r.Hours, n); len(hours) > 0 {
		screens = append(screens, func(img *image.RGBA) error { return d.hours(img, hours, n) })
	}

	return show(ctx, canvas, b.BoardDelay, screens)
}

// Render shows the coming days, starting tomorrow, a panel's width at a time.
func (b *ForecastBoard) Render(ctx context.Context, canvas board.Canvas) error {
	if !b.enabler.Enabled() {
		return nil
	}

	r := b.report(ctx)
	if r == nil {
		return nil
	}

	d, err := b.draw.setup(canvas.Bounds())
	if err != nil {
		return err
	}

	days := upcomingDays(r.Days, time.Now(), b.config.Days)
	if len(days) == 0 {
		return nil
	}

	perScreen := columns(canvas.Bounds(), d.scale)
	var screens []func(*image.RGBA) error
	for start := 0; start < len(days); start += perScreen {
		page := days[start:min(start+perScreen, len(days))]
		screens = append(screens, func(img *image.RGBA) error { return d.days(img, page, perScreen) })
	}

	return show(ctx, canvas, b.BoardDelay, screens)
}

// columns is how many hours or days fit across a panel.
func columns(bounds image.Rectangle, scale int) int {
	n := bounds.Dx() / (columnWidth * scale)
	if n < 1 {
		n = 1
	}
	return n
}

// pickHours takes n hours, three apart.
func pickHours(hours []weather.Conditions, n int) []weather.Conditions {
	var out []weather.Conditions
	for i := 0; i < len(hours) && len(out) < n; i += 3 {
		out = append(out, hours[i])
	}
	return out
}

// upcomingDays are up to n days after today.
func upcomingDays(days []weather.Day, now time.Time, n int) []weather.Day {
	today := dateOf(now)
	var out []weather.Day
	for _, day := range days {
		if !dateOf(day.Date).After(today) {
			continue
		}
		out = append(out, day)
		if len(out) == n {
			break
		}
	}
	return out
}

// dateOf is the calendar date of t, where t is.
func dateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// temp writes a temperature as a whole number, like 62. The degree mark is
// drawn separately, since the panel's fonts don't have one.
func temp(t float64) string {
	return fmt.Sprintf("%d", int(math.Round(t)))
}

// now draws the weather now: a big icon, the temperature, today's high and
// low, and what it's doing.
func (d *drawer) now(img *image.RGBA, r *weather.Report, now time.Time) error {
	s := d.scale
	bounds := img.Bounds()

	drawIcon(img, bounds.Min, iconFor(r.Now.Kind, r.Now.Day), 2*s)

	right := image.Rect(bounds.Min.X+2*iconSize*s+s, bounds.Min.Y+s, bounds.Max.X, bounds.Min.Y+14*s)
	if err := d.centeredTemp(img, d.big, right, temp(r.Now.Temp), white); err != nil {
		return err
	}

	if len(r.Days) > 0 && dateOf(r.Days[0].Date).Equal(dateOf(now)) {
		today := r.Days[0]
		var parts []string
		var clrs []color.RGBA
		if today.HasHigh {
			parts, clrs = append(parts, temp(today.High)), append(clrs, warm)
		}
		if today.HasLow {
			parts, clrs = append(parts, temp(today.Low)), append(clrs, cool)
		}
		hilo := image.Rect(right.Min.X, right.Max.Y+2*s, right.Max.X, right.Max.Y+8*s)
		if err := d.tempPair(img, hilo, parts, clrs); err != nil {
			return err
		}
	}

	// a line up from the bottom, so that the tails of g and y fit
	text := d.fitText(img, r.Now.Text, bounds.Dx())
	line := image.Rect(bounds.Min.X, bounds.Max.Y-8*s, bounds.Max.X, bounds.Max.Y)
	return d.centered(img, d.small, line, text, dim)
}

// hours draws a column for each hour, of n across the panel: when, what, and
// how warm.
func (d *drawer) hours(img *image.RGBA, hours []weather.Conditions, n int) error {
	s := d.scale
	for i, h := range hours {
		col := column(img.Bounds(), i, n)
		if err := d.columnTop(img, col, hourLabel(h.Start.Local()), h.Kind, h.Day); err != nil {
			return err
		}
		if err := d.centeredTemp(img, d.small, row(col, 20*s, 6*s), temp(h.Temp), white); err != nil {
			return err
		}
		if h.PrecipChance >= 20 {
			if err := d.centered(img, d.small, row(col, 26*s, 6*s), fmt.Sprintf("%d%%", h.PrecipChance), rainBlue); err != nil {
				return err
			}
		}
	}
	return nil
}

// days draws a column for each day, of n across the panel: which, what, the
// high and the low. The last page of a forecast can have fewer days than
// columns, and keeps the same columns so it lines up with the page before.
func (d *drawer) days(img *image.RGBA, days []weather.Day, n int) error {
	s := d.scale
	for i, day := range days {
		col := column(img.Bounds(), i, n)
		if err := d.columnTop(img, col, strings.ToUpper(day.Date.Format("Mon")), day.Kind, true); err != nil {
			return err
		}
		if day.HasHigh {
			if err := d.centeredTemp(img, d.small, row(col, 20*s, 6*s), temp(day.High), warm); err != nil {
				return err
			}
		}
		if day.HasLow {
			if err := d.centeredTemp(img, d.small, row(col, 26*s, 6*s), temp(day.Low), cool); err != nil {
				return err
			}
		}
	}
	return nil
}

// column is the i'th of n equal columns across bounds.
func column(bounds image.Rectangle, i, n int) image.Rectangle {
	w := bounds.Dx() / n
	return image.Rect(bounds.Min.X+i*w, bounds.Min.Y, bounds.Min.X+(i+1)*w, bounds.Max.Y)
}

// row is the strip of col from y down h pixels.
func row(col image.Rectangle, y, h int) image.Rectangle {
	return image.Rect(col.Min.X, col.Min.Y+y, col.Max.X, col.Min.Y+y+h)
}

// columnTop draws a column's label and icon.
func (d *drawer) columnTop(img *image.RGBA, col image.Rectangle, label string, kind weather.Kind, day bool) error {
	s := d.scale
	if err := d.centered(img, d.small, row(col, 0, 6*s), label, labelGold); err != nil {
		return err
	}
	iconX := col.Min.X + (col.Dx()-iconSize*s)/2
	drawIcon(img, image.Pt(iconX, col.Min.Y+7*s), iconFor(kind, day), s)
	return nil
}

// text writes str with its top left corner at pt.
func (d *drawer) text(img *image.RGBA, w *rgbrender.TextWriter, pt image.Point, str string, width int, clr color.RGBA) error {
	box := image.Rect(pt.X, pt.Y, pt.X+width+1, img.Bounds().Max.Y)
	return w.WriteAligned(rgbrender.LeftTop, img, box, []string{str}, clr)
}

func (d *drawer) width(img *image.RGBA, w *rgbrender.TextWriter, str string) (int, error) {
	widths, err := w.MeasureStrings(img, []string{str})
	if err != nil {
		return 0, err
	}
	return widths[0], nil
}

// centered writes str centered across box, at its top.
func (d *drawer) centered(img *image.RGBA, w *rgbrender.TextWriter, box image.Rectangle, str string, clr color.RGBA) error {
	width, err := d.width(img, w, str)
	if err != nil {
		return err
	}
	return d.text(img, w, image.Pt(box.Min.X+(box.Dx()-width)/2, box.Min.Y), str, width, clr)
}

// degree is the size of the degree mark after a temperature, and the gap
// before it.
func (d *drawer) degree(w *rgbrender.TextWriter) (int, int) {
	if w == d.big {
		return 3 * d.scale, d.scale
	}
	return 2 * d.scale, 0
}

// centeredTemp writes a temperature and its degree mark centered across box.
func (d *drawer) centeredTemp(img *image.RGBA, w *rgbrender.TextWriter, box image.Rectangle, t string, clr color.RGBA) error {
	width, err := d.width(img, w, t)
	if err != nil {
		return err
	}
	size, gap := d.degree(w)
	x := box.Min.X + (box.Dx()-width-gap-size)/2
	return d.tempAt(img, w, image.Pt(x, box.Min.Y), t, width, clr)
}

// tempAt writes a temperature at pt, then its degree mark: a small square
// ring at the top right of the number.
func (d *drawer) tempAt(img *image.RGBA, w *rgbrender.TextWriter, pt image.Point, t string, width int, clr color.RGBA) error {
	if err := d.text(img, w, pt, t, width, clr); err != nil {
		return err
	}
	size, gap := d.degree(w)
	x := pt.X + width + gap
	for i := 0; i < size; i++ {
		img.SetRGBA(x+i, pt.Y, clr)
		img.SetRGBA(x+i, pt.Y+size-1, clr)
		img.SetRGBA(x, pt.Y+i, clr)
		img.SetRGBA(x+size-1, pt.Y+i, clr)
	}
	return nil
}

// tempPair writes one or two temperatures side by side, centered across box.
func (d *drawer) tempPair(img *image.RGBA, box image.Rectangle, parts []string, clrs []color.RGBA) error {
	if len(parts) == 0 {
		return nil
	}
	space := 3 * d.scale
	size, gap := d.degree(d.small)

	widths := make([]int, len(parts))
	total := space * (len(parts) - 1)
	for i, p := range parts {
		w, err := d.width(img, d.small, p)
		if err != nil {
			return err
		}
		widths[i] = w
		total += w + gap + size
	}

	x := box.Min.X + (box.Dx()-total)/2
	for i, p := range parts {
		if err := d.tempAt(img, d.small, image.Pt(x, box.Min.Y), p, widths[i], clrs[i]); err != nil {
			return err
		}
		x += widths[i] + gap + size + space
	}
	return nil
}

// hourLabel is an hour as it fits in a column: 9A, 12P.
func hourLabel(t time.Time) string {
	h := t.Hour()
	suffix := "A"
	if h >= 12 {
		suffix = "P"
	}
	h %= 12
	if h == 0 {
		h = 12
	}
	return fmt.Sprintf("%d%s", h, suffix)
}

// fitText shortens a forecast's words until they fit width: the Weather
// Service's "Isolated Rain Showers then Mostly Cloudy" becomes "Rain Showers".
func (d *drawer) fitText(img *image.RGBA, text string, width int) string {
	fits := func(s string) bool {
		w, err := d.width(img, d.small, s)
		return err == nil && w <= width
	}

	text = strings.TrimSpace(text)
	if fits(text) {
		return text
	}
	if first, _, ok := strings.Cut(text, " then "); ok {
		text = first
		if fits(text) {
			return text
		}
	}

	words := strings.Fields(text)
	for len(words) > 1 {
		words = words[1:]
		if strings.EqualFold(words[0], "and") || strings.EqualFold(words[0], "of") {
			continue
		}
		if s := strings.Join(words, " "); fits(s) {
			return s
		}
	}

	// one word that still doesn't fit
	runes := []rune(text)
	for len(runes) > 0 && !fits(string(runes)) {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
