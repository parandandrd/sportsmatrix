package logo

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

// ESPN's logos sit in a square with transparent margins of their own size.
// Fitting goes by the logo itself, not the square.
func TestFitForBlackTrimsMarginsAndKeepsShape(t *testing.T) {
	t.Parallel()

	red := color.NRGBA{R: 255, A: 255}
	src := image.NewNRGBA(image.Rect(0, 0, 200, 200))
	draw.Draw(src, image.Rect(40, 60, 140, 110), &image.Uniform{red}, image.Point{}, draw.Src)

	got := FitForBlack(src, 28, 28)
	if got.Bounds().Dx() != 28 || got.Bounds().Dy() != 14 {
		t.Fatalf("100x50 logo fit to %v, want 28x14", got.Bounds())
	}
	for y := 0; y < 14; y++ {
		for x := 0; x < 28; x++ {
			if c := got.NRGBAAt(x, y); c != red {
				t.Fatalf("pixel %d,%d is %v, want %v", x, y, c, red)
			}
		}
	}
}

// Light adds up in linear terms. Half white and half black is a mid grey of
// 188 in sRGB; averaging the sRGB values says 128, which is what made thin
// bright lines go dim when logos were shrunk.
func TestFitForBlackAveragesInLinearLight(t *testing.T) {
	t.Parallel()

	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	src.SetNRGBA(1, 0, color.NRGBA{A: 255})

	got := FitForBlack(src, 1, 1).NRGBAAt(0, 0)
	want := color.NRGBA{R: 188, G: 188, B: 188, A: 255}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A pixel the logo only partly covers must still come out right once drawn
// onto the black panel with image/draw, which blends in sRGB.
func TestFitForBlackEdgesOverBlack(t *testing.T) {
	t.Parallel()

	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	src := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	src.SetNRGBA(0, 0, white)
	src.SetNRGBA(2, 0, white)

	logo := FitForBlack(src, 1, 1)
	panel := image.NewRGBA(image.Rect(0, 0, 1, 1))
	draw.Draw(panel, panel.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	draw.Draw(panel, panel.Bounds(), logo, image.Point{}, draw.Over)

	want := int(linearToSRGB(2.0 / 3))
	got := panel.RGBAAt(0, 0)
	for _, v := range []uint8{got.R, got.G, got.B} {
		if d := int(v) - want; d < -1 || d > 1 {
			t.Fatalf("two thirds of white drew as %v, want %d", got, want)
		}
	}
}

func TestFitForBlackNothingOpaque(t *testing.T) {
	t.Parallel()

	if got := FitForBlack(image.NewNRGBA(image.Rect(0, 0, 10, 10)), 28, 28); got != nil {
		t.Errorf("fully transparent logo gave %v", got.Bounds())
	}
}

// The cached source is an ordinary image, not one made for drawing over black:
// transparent stays transparent and opaque colors keep their value.
func TestShrinkSourceKeepsColors(t *testing.T) {
	t.Parallel()

	blue := color.NRGBA{R: 10, G: 40, B: 120, A: 255}
	src := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	draw.Draw(src, image.Rect(0, 0, 256, 512), &image.Uniform{blue}, image.Point{}, draw.Src)

	got := ShrinkSource(src).(*image.NRGBA)
	if c := got.NRGBAAt(10, 10); c != blue {
		t.Errorf("opaque pixel %v, want %v", c, blue)
	}
	if c := got.NRGBAAt(200, 10); c.A != 0 {
		t.Errorf("transparent pixel %v, want alpha 0", c)
	}
}
