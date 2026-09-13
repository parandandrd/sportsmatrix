package scrollcanvas

import (
	"image"
	"image/color"
)

func isBlack(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r == 0 && b == 0 && g == 0
}

// blankScanner returns a "is the pixel at x,y black?" func for img.
//
// The helpers below scan whole canvases, and image.Image.At boxes its return
// value into a color.Color interface -- a heap allocation per pixel. For the
// *image.RGBA that every canvas here actually is, reading the raw bytes gives
// the same answer: color.RGBA.RGBA() only scales each 8-bit channel up to 16
// bits, so a channel is zero in one representation exactly when it's zero in
// the other. Out of bounds counts as blank, matching At.
func blankScanner(img image.Image) func(x, y int) bool {
	rgba, ok := img.(*image.RGBA)
	if !ok {
		return func(x, y int) bool {
			return isBlack(img.At(x, y))
		}
	}

	return func(x, y int) bool {
		if !(image.Point{X: x, Y: y}.In(rgba.Rect)) {
			return true
		}
		i := rgba.PixOffset(x, y)

		return rgba.Pix[i] == 0 && rgba.Pix[i+1] == 0 && rgba.Pix[i+2] == 0
	}
}

func firstNonBlankY(img image.Image) int {
	if img == nil {
		return 0
	}
	blank := blankScanner(img)
	for y := img.Bounds().Min.Y; y <= img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x <= img.Bounds().Max.X; x++ {
			if !blank(x, y) {
				return y
			}
		}
	}

	return img.Bounds().Min.Y
}

func firstNonBlankX(img image.Image) int {
	if img == nil {
		return 0
	}
	blank := blankScanner(img)
	for x := img.Bounds().Min.X; x <= img.Bounds().Max.X; x++ {
		for y := img.Bounds().Min.Y; y <= img.Bounds().Max.Y; y++ {
			if !blank(x, y) {
				return x
			}
		}
	}

	return img.Bounds().Min.X
}

func lastNonBlankY(img image.Image) int {
	if img == nil {
		return 0
	}
	blank := blankScanner(img)
	for y := img.Bounds().Max.Y; y >= img.Bounds().Min.Y; y-- {
		for x := img.Bounds().Min.X; x <= img.Bounds().Max.X; x++ {
			if !blank(x, y) {
				return y
			}
		}
	}

	return img.Bounds().Max.Y
}

func lastNonBlankX(img image.Image) int {
	if img == nil {
		return 0
	}
	blank := blankScanner(img)
	for x := img.Bounds().Max.X; x >= img.Bounds().Min.X; x-- {
		for y := img.Bounds().Max.Y; y >= img.Bounds().Min.Y; y-- {
			if !blank(x, y) {
				return x
			}
		}
	}

	return img.Bounds().Max.X
}

// nonBlankXRange returns firstNonBlankX and lastNonBlankX in a single pass,
// so building subcanvases doesn't scan every canvas twice.
func nonBlankXRange(img image.Image) (int, int) {
	if img == nil {
		return 0, 0
	}

	blank := blankScanner(img)
	bounds := img.Bounds()
	first := bounds.Min.X
	last := bounds.Max.X

	foundFirst := false
	for x := bounds.Min.X; x <= bounds.Max.X; x++ {
		for y := bounds.Min.Y; y <= bounds.Max.Y; y++ {
			if !blank(x, y) {
				if !foundFirst {
					first = x
					foundFirst = true
				}
				last = x
				break
			}
		}
	}

	return first, last
}
