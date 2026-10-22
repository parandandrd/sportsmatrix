package logo

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
)

// trimAlpha is how opaque a pixel must be to count as part of the logo when
// its transparent margins are trimmed. ESPN's art fades out over a pixel or
// two at its edges; this keeps those faint pixels from holding the margins
// open.
const trimAlpha = 16

// srgbToLinear maps an 8-bit sRGB value to linear light, 0 to 1.
var srgbToLinear = func() [256]float32 {
	var t [256]float32
	for i := range t {
		c := float64(i) / 255
		if c <= 0.04045 {
			t[i] = float32(c / 12.92)
		} else {
			t[i] = float32(math.Pow((c+0.055)/1.055, 2.4))
		}
	}
	return t
}()

// linearToSRGB maps linear light, 0 to 1, back to an 8-bit sRGB value.
func linearToSRGB(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	var c float64
	if v <= 0.0031308 {
		c = float64(v) * 12.92
	} else {
		c = 1.055*math.Pow(float64(v), 1/2.4) - 0.055
	}
	return uint8(math.Round(c * 255))
}

// opaqueBounds is the smallest rectangle holding every pixel of img at least
// trimAlpha opaque. It is empty if there is none.
func opaqueBounds(img image.Image) image.Rectangle {
	b := img.Bounds()
	out := image.Rectangle{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA).A < trimAlpha {
				continue
			}
			out = out.Union(image.Rect(x, y, x+1, y+1))
		}
	}
	return out
}

// tap is one source pixel's share of a destination pixel.
type tap struct {
	i int
	w float32
}

// areaTaps says, for each of dst pixels along one axis, which of src source
// pixels it covers and by how much. Each destination pixel averages exactly
// the area it covers, so a large reduction neither drops detail between
// samples nor rings at hard edges the way Lanczos does. Enlarging, which a
// logo rarely needs, comes out as nearest-neighbour.
func areaTaps(src, dst int) [][]tap {
	scale := float64(src) / float64(dst)
	taps := make([][]tap, dst)
	for d := range taps {
		lo, hi := float64(d)*scale, float64(d+1)*scale
		for i := int(lo); i < src && float64(i) < hi; i++ {
			w := math.Min(hi, float64(i+1)) - math.Max(lo, float64(i))
			if w > 0 {
				taps[d] = append(taps[d], tap{i: i, w: float32(w / (hi - lo))})
			}
		}
	}
	return taps
}

// resizeLinear scales the part of img inside r to w x h. It averages in
// linear light with the color premultiplied by alpha, which is what light
// mixing on the panel does: averaging the stored sRGB values instead darkens
// thin bright lines and muddies the colors where two meet. The result is
// four float32s a pixel, linear and premultiplied.
//
// It goes across a row at a time, so it never holds the whole source in
// floats: some of ESPN's logos are 4096x4096, which would be 256MB.
func resizeLinear(img image.Image, r image.Rectangle, w, h int) []float32 {
	sw, sh := r.Dx(), r.Dy()
	row := make([]float32, sw*4)
	across := make([]float32, w*sh*4)
	xTaps := areaTaps(sw, w)
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			c := color.NRGBAModel.Convert(img.At(r.Min.X+x, r.Min.Y+y)).(color.NRGBA)
			a := float32(c.A) / 255
			row[x*4+0] = srgbToLinear[c.R] * a
			row[x*4+1] = srgbToLinear[c.G] * a
			row[x*4+2] = srgbToLinear[c.B] * a
			row[x*4+3] = a
		}
		for x, taps := range xTaps {
			o := (y*w + x) * 4
			for _, t := range taps {
				for c := 0; c < 4; c++ {
					across[o+c] += row[t.i*4+c] * t.w
				}
			}
		}
	}

	// Then down.
	out := make([]float32, w*h*4)
	for y, taps := range areaTaps(sh, h) {
		for x := 0; x < w; x++ {
			o := (y*w + x) * 4
			for _, t := range taps {
				s := (t.i*w + x) * 4
				for c := 0; c < 4; c++ {
					out[o+c] += across[s+c] * t.w
				}
			}
		}
	}
	return out
}

// toNRGBA turns resizeLinear's output into an ordinary image.
//
// With forBlack set, a partly transparent pixel is stored so that drawing it
// over black lands on the color the linear average says it should. image/draw
// blends in sRGB, which over black is the stored value scaled by alpha, so a
// plain conversion makes every anti-aliased edge darker than it should be:
// half of white comes out 128 instead of 188. Over black only the product of
// color and alpha matters, so alpha is raised where the color alone cannot
// reach. Team logos are always drawn first, straight onto the black panel, so
// this is exact for them; anything drawn over something else wants it off.
func toNRGBA(px []float32, w, h int, forBlack bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		p := px[i*4 : i*4+4]
		a := min(p[3], 1)
		if a <= 0 {
			continue
		}
		o := i * 4
		if !forBlack {
			for c := 0; c < 3; c++ {
				img.Pix[o+c] = linearToSRGB(p[c] / a)
			}
			img.Pix[o+3] = uint8(math.Round(float64(a) * 255))
			continue
		}

		var target [3]float64
		alpha := float64(a)
		for c := 0; c < 3; c++ {
			target[c] = float64(linearToSRGB(p[c]))
			alpha = math.Max(alpha, target[c]/255)
		}
		a8 := uint8(math.Round(alpha * 255))
		if a8 == 0 {
			continue
		}
		for c := 0; c < 3; c++ {
			img.Pix[o+c] = uint8(math.Min(255, math.Round(target[c]*255/float64(a8))))
		}
		img.Pix[o+3] = a8
	}
	return img
}

// fitSize is the largest size with the shape of w x h that fits in maxW x
// maxH, never less than a pixel either way.
func fitSize(w, h, maxW, maxH int) (int, int) {
	scale := math.Min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	fw := min(maxW, max(1, int(math.Round(float64(w)*scale))))
	fh := min(maxH, max(1, int(math.Round(float64(h)*scale))))
	return fw, fh
}

// FitForBlack trims img's transparent margins and scales what is left to fit
// inside maxW x maxH, keeping its shape, for drawing onto the black panel. It
// returns nil if img has nothing opaque in it.
func FitForBlack(img image.Image, maxW, maxH int) *image.NRGBA {
	r := opaqueBounds(img)
	if r.Empty() || maxW < 1 || maxH < 1 {
		return nil
	}
	w, h := fitSize(r.Dx(), r.Dy(), maxW, maxH)
	return toNRGBA(resizeLinear(img, r, w, h), w, h, true)
}

// Fit returns the logo trimmed of its transparent margins and scaled to fit
// inside maxW x maxH, ready to draw onto the black panel. Each size is made
// once and kept: the source is already cached on disk, and making one from it
// takes a few milliseconds.
func (l *Logo) Fit(ctx context.Context, maxW, maxH int) (image.Image, error) {
	key := image.Pt(maxW, maxH)

	l.fitLock.Lock()
	defer l.fitLock.Unlock()
	if img, ok := l.fitted[key]; ok {
		return img, nil
	}

	if l.sourceLogoGetter == nil {
		return nil, fmt.Errorf("sourceLogoGetter was nil")
	}
	src, err := l.sourceLogoGetter(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get logo source for %s: %w", l.key, err)
	}
	if src == nil {
		return nil, fmt.Errorf("failed to get logo source for %s", l.key)
	}

	img := FitForBlack(src, maxW, maxH)
	if img == nil {
		return nil, fmt.Errorf("logo %s is fully transparent", l.key)
	}

	if l.fitted == nil {
		l.fitted = make(map[image.Point]image.Image)
	}
	l.fitted[key] = img
	return img, nil
}
