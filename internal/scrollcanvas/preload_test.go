package scrollcanvas

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/robbydyer/sports/internal/matrix"
)

// testActual builds a canvas with deterministic pixel content and blank
// margins, so the blank-trimming in PrepareSubCanvases has something to find.
func testActual(seed int, w int, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			if x < 3+seed || x > w-(2+seed) {
				continue
			}
			img.Set(x, y, color.RGBA{
				R: uint8((x*7 + y*3 + seed*11) % 256),
				G: uint8((x*13 + y*5 + seed*3) % 256),
				B: uint8((x*3 + y*17 + seed*7) % 256),
				A: 255,
			})
		}
	}

	return img
}

func testCanvas(t testing.TB, numActuals int, w int, h int) *ScrollCanvas {
	t.Helper()

	l := zap.NewNop()
	c, err := NewScrollCanvas(matrix.NewConsoleMatrix(w, h, io.Discard, l), l, WithMergePadding(12))
	require.NoError(t, err)

	for i := 0; i < numActuals; i++ {
		c.AddCanvas(testActual(i, w, h))
	}

	return c
}

// referenceActualPixel is how master resolved a scroll pixel: scan every
// subcanvas in order and use image.Image.At. The span-based frame fill has to
// agree with it exactly, so it stays here as the definition of correct.
func (c *ScrollCanvas) referenceActualPixel(virtualX int, virtualY int) color.Color {
	for _, sub := range c.subCanvases {
		if sub == nil {
			continue
		}
		if virtualX >= sub.virtualStartX && virtualX <= sub.virtualEndX {
			actualX := (virtualX - sub.virtualStartX) + sub.actualStartX
			return sub.img.At(actualX, virtualY)
		}
	}

	return color.Black
}

// The span decomposition must reproduce master's per-pixel lookup exactly, for
// every column of every frame, including the frames that run off the end of the
// content.
func TestFillHorizontalFrameMatchesReference(t *testing.T) {
	t.Parallel()

	c := testCanvas(t, 4, 32, 16)
	c.PrepareSubCanvases()

	last := c.subCanvases[len(c.subCanvases)-1]
	require.NotNil(t, last, "last subcanvas must never be nil")

	buf := &preloadBuf{points: make([]matrix.MatrixPoint, c.w*c.h)}

	for virtualXStart := 0; virtualXStart <= last.virtualEndX; virtualXStart++ {
		c.fillHorizontalFrame(buf, virtualXStart)

		for y := 0; y < c.h; y++ {
			for x := 0; x < c.w; x++ {
				wantR, wantG, wantB, wantA := c.referenceActualPixel(x+virtualXStart, y).RGBA()
				got := buf.points[y*c.w+x]
				gotR, gotG, gotB, gotA := got.Color.RGBA()

				require.Equal(t, matrix.MatrixPoint{X: x, Y: y, Color: got.Color}, got,
					"point coordinates wrong at frame %d, x=%d y=%d", virtualXStart, x, y)
				require.Equal(t,
					[]uint32{wantR, wantG, wantB, wantA},
					[]uint32{gotR, gotG, gotB, gotA},
					"pixel mismatch at frame %d, x=%d y=%d", virtualXStart, x, y,
				)
			}
		}
	}
}

// A nil entry in actuals used to leave trailing nils in subCanvases, which made
// the last element nil and made horizontalPrep bail out entirely.
func TestPrepareSubCanvasesSkipsNilActuals(t *testing.T) {
	t.Parallel()

	c := testCanvas(t, 3, 32, 16)
	c.actuals[1] = nil
	c.PrepareSubCanvases()

	for i, sub := range c.subCanvases {
		require.NotNil(t, sub, "subcanvas %d was nil", i)
	}

	require.NoError(t, c.horizontalPrep(context.Background()))
}

func TestNonBlankXRangeMatchesSinglePassHelpers(t *testing.T) {
	t.Parallel()

	for seed := 0; seed < 4; seed++ {
		img := testActual(seed, 32, 16)
		first, last := nonBlankXRange(img)
		require.Equal(t, firstNonBlankX(img), first, "first, seed %d", seed)
		require.Equal(t, lastNonBlankX(img), last, "last, seed %d", seed)
	}

	blank := image.NewRGBA(image.Rect(0, 0, 32, 16))
	first, last := nonBlankXRange(blank)
	require.Equal(t, firstNonBlankX(blank), first)
	require.Equal(t, lastNonBlankX(blank), last)
}

// The blank scanners have a fast path for *image.RGBA; make sure it agrees with
// the generic image.Image path.
func TestBlankScannerFastPathMatchesGeneric(t *testing.T) {
	t.Parallel()

	rgba := testActual(1, 32, 16)
	generic := image.Image(&genericImage{rgba})

	require.Equal(t, firstNonBlankX(generic), firstNonBlankX(rgba))
	require.Equal(t, lastNonBlankX(generic), lastNonBlankX(rgba))
	require.Equal(t, firstNonBlankY(generic), firstNonBlankY(rgba))
	require.Equal(t, lastNonBlankY(generic), lastNonBlankY(rgba))
}

// genericImage hides the concrete *image.RGBA so the generic At-based path runs.
type genericImage struct {
	img *image.RGBA
}

func (g *genericImage) ColorModel() color.Model { return g.img.ColorModel() }
func (g *genericImage) Bounds() image.Rectangle { return g.img.Bounds() }
func (g *genericImage) At(x, y int) color.Color { return g.img.At(x, y) }

func TestVerticalPrepRuns(t *testing.T) {
	t.Parallel()

	l := zaptest.NewLogger(t)
	c, err := NewScrollCanvas(matrix.NewConsoleMatrix(32, 16, io.Discard, l), l,
		WithScrollDirection(BottomToTop))
	require.NoError(t, err)

	c.SetPadding(16)
	draw.Draw(c.actual, image.Rect(0, 0, 32, 16), testActual(0, 32, 16), image.Point{}, draw.Src)

	require.NoError(t, c.verticalPrep(context.Background()))
}

func BenchmarkHorizontalPrep(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		c := testCanvas(b, 4, 128, 32)
		b.StartTimer()

		if err := c.horizontalPrep(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerticalPrep(b *testing.B) {
	ctx := context.Background()
	l := zap.NewNop()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		c, err := NewScrollCanvas(matrix.NewConsoleMatrix(128, 32, io.Discard, l), l,
			WithScrollDirection(BottomToTop))
		require.NoError(b, err)
		c.SetPadding(160)
		draw.Draw(c.actual, image.Rect(0, 0, 128, 32), testActual(0, 128, 32), image.Point{}, draw.Src)
		b.StartTimer()

		if err := c.verticalPrep(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNonBlankXRange(b *testing.B) {
	img := testActual(1, 128, 32)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = nonBlankXRange(img)
	}
}
