package tvboard

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"sync"
	"time"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/rgbrender"
	"github.com/parandandrd/sportsmatrix/internal/tvmaze"
)

var (
	white    = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	dim      = color.RGBA{R: 170, G: 170, B: 170, A: 255}
	gold     = color.RGBA{R: 255, G: 200, B: 80, A: 255}
	premiere = color.RGBA{R: 255, G: 110, B: 60, A: 255}
	special  = color.RGBA{R: 200, G: 120, B: 255, A: 255}
	fresh    = color.RGBA{R: 90, G: 220, B: 110, A: 255}
	when     = color.RGBA{R: 110, G: 170, B: 255, A: 255}
)

// Render shows each new episode coming up in turn, one to a screen.
func (b *Board) Render(ctx context.Context, canvas board.Canvas) error {
	if !b.enabler.Enabled() {
		return nil
	}

	eps := b.upcoming(ctx)
	if len(eps) == 0 {
		return nil
	}

	d, err := b.draw.setup(canvas.Bounds())
	if err != nil {
		return err
	}

	for _, ep := range eps {
		select {
		case <-ctx.Done():
			return context.Canceled
		default:
		}

		shown := time.Now()
		img := image.NewRGBA(canvas.Bounds())
		if err := d.episode(img, ep, b.now()); err != nil {
			return err
		}
		draw.Draw(canvas, canvas.Bounds(), img, canvas.Bounds().Min, draw.Over)
		if err := canvas.Render(ctx); err != nil {
			return err
		}

		if err := board.Hold(ctx, shown, b.BoardDelay()); err != nil {
			return err
		}
	}

	return nil
}

// drawer holds the text writer for the size of panel the board draws on.
type drawer struct {
	lock  sync.Mutex
	scale int
	text  *rgbrender.TextWriter
}

func (d *drawer) setup(bounds image.Rectangle) (*drawer, error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	scale := max(bounds.Dy()/32, 1)
	if d.text != nil && d.scale == scale {
		return d, nil
	}

	fnt, err := rgbrender.GetFont("04B_03__.ttf")
	if err != nil {
		return nil, err
	}
	w := rgbrender.NewTextWriter(fnt, 8*float64(scale))
	w.YStartCorrection = -2 * scale

	d.scale, d.text = scale, w
	return d, nil
}

// episode draws one episode: the show, which episode, and when and where it
// airs.
//
//	9-1-1: NASHVILLE
//	S2E1 PREMIERE
//	THU OCT 15
//	8PM ABC
func (d *drawer) episode(img *image.RGBA, ep tvmaze.Episode, now time.Time) error {
	s := d.scale
	bounds := img.Bounds()
	width := bounds.Dx()

	title := d.wrap(img, strings.ToUpper(ep.Show.Name), width, 2)
	y := bounds.Min.Y
	if len(title) == 1 {
		y += 3 * s
	}
	for _, line := range title {
		if err := d.centered(img, bounds, y, []string{line}, []color.RGBA{gold}); err != nil {
			return err
		}
		y += 6 * s
	}

	which := fmt.Sprintf("S%dE%d", ep.Season, ep.Number)
	if ep.Number == 0 {
		which = fmt.Sprintf("S%d", ep.Season)
	}
	tag, tagColor := "NEW", fresh
	switch {
	case ep.Special:
		tag, tagColor = "SPECIAL", special
	case ep.Premiere():
		tag, tagColor = "PREMIERE", premiere
	}
	if err := d.centered(img, bounds, bounds.Min.Y+13*s, []string{which, tag}, []color.RGBA{dim, tagColor}); err != nil {
		return err
	}

	if err := d.centered(img, bounds, bounds.Min.Y+19*s, []string{dayLabel(ep, now)}, []color.RGBA{when}); err != nil {
		return err
	}

	var parts []string
	var clrs []color.RGBA
	if ep.Timed {
		parts, clrs = append(parts, clockLabel(ep.Airs.In(now.Location()))), append(clrs, white)
	}
	if ep.Show.Network != "" {
		network := d.fit(img, strings.ToUpper(ep.Show.Network), width-d.width(img, strings.Join(parts, " "))-4*s)
		if network != "" {
			parts, clrs = append(parts, network), append(clrs, dim)
		}
	}
	return d.centered(img, bounds, bounds.Min.Y+25*s, parts, clrs)
}

// dayLabel says when an episode is on, as the day it is: ON NOW, TODAY,
// TOMORROW, or THU OCT 15.
func dayLabel(ep tvmaze.Episode, now time.Time) string {
	airs := ep.Airs.In(now.Location())
	if ep.Timed && !airs.After(now) {
		return "ON NOW"
	}
	days := dayNumber(airs) - dayNumber(now)
	switch days {
	case 0:
		return "TODAY"
	case 1:
		return "TOMORROW"
	}
	return strings.ToUpper(airs.Format("Mon Jan 2"))
}

// dayNumber counts days, so that two times' difference is the number of
// midnights between them.
func dayNumber(t time.Time) int {
	y, m, d := t.Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// clockLabel is a time as it fits the panel: 8PM, 8:30PM.
func clockLabel(t time.Time) string {
	h, m := t.Hour(), t.Minute()
	suffix := "AM"
	if h >= 12 {
		suffix = "PM"
	}
	h %= 12
	if h == 0 {
		h = 12
	}
	if m == 0 {
		return fmt.Sprintf("%d%s", h, suffix)
	}
	return fmt.Sprintf("%d:%02d%s", h, m, suffix)
}

func (d *drawer) width(img *image.RGBA, s string) int {
	if s == "" {
		return 0
	}
	w, err := d.text.MeasureStrings(img, []string{s})
	if err != nil {
		return 0
	}
	return w[0]
}

// centered writes words side by side, a space apart, each in its own color,
// centered across bounds with their tops at y.
func (d *drawer) centered(img *image.RGBA, bounds image.Rectangle, y int, words []string, clrs []color.RGBA) error {
	space := d.width(img, " ")
	total := 0
	for i, w := range words {
		if i > 0 {
			total += space
		}
		total += d.width(img, w)
	}

	x := bounds.Min.X + (bounds.Dx()-total)/2
	for i, w := range words {
		width := d.width(img, w)
		// AlignPosition centers from x=0 rather than from the box, so place
		// each word at its left edge
		box := image.Rect(x, y, x+width+1, bounds.Max.Y)
		if err := d.text.WriteAligned(rgbrender.LeftTop, img, box, []string{w}, clrs[i]); err != nil {
			return err
		}
		x += width + space
	}
	return nil
}

// wrap breaks text into at most lines lines that fit width, cutting the last
// short when it doesn't all fit.
func (d *drawer) wrap(img *image.RGBA, text string, width, lines int) []string {
	var out []string
	words := strings.Fields(text)
	for len(words) > 0 && len(out) < lines {
		line := words[0]
		n := 1
		for n < len(words) && d.width(img, line+" "+words[n]) <= width {
			line += " " + words[n]
			n++
		}
		words = words[n:]
		if len(out) == lines-1 && len(words) > 0 {
			line += " " + strings.Join(words, " ")
			words = nil
		}
		out = append(out, d.fit(img, line, width))
	}
	return out
}

// fit cuts text short until it fits width.
func (d *drawer) fit(img *image.RGBA, text string, width int) string {
	runes := []rune(strings.TrimSpace(text))
	for len(runes) > 0 && d.width(img, string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return strings.TrimSpace(string(runes))
}
