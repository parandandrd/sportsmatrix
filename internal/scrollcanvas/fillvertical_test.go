package scrollcanvas

import (
	"image"
	"image/draw"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/matrix"
)

// referenceFillVerticalFrame is the straightforward per-pixel version: clamp
// the loop, then look up each pixel through rgbaAt, which bounds-checks and
// recomputes its offset every time. The optimized fill has to agree with it
// exactly, for every frame of a scroll, so it stays here as the definition of
// correct.
func (c *ScrollCanvas) referenceFillVerticalFrame(yShift int) []matrix.MatrixPoint {
	bounds := c.actual.Bounds()
	points := make([]matrix.MatrixPoint, 0, c.w*c.h)

	xLo := maxInt(bounds.Min.X, 1)
	xHi := minInt(bounds.Max.X, c.w-1)
	yLo := maxInt(bounds.Min.Y, 1-yShift)
	yHi := minInt(bounds.Max.Y, c.h-1-yShift)

	for y := yLo; y <= yHi; y++ {
		for x := xLo; x <= xHi; x++ {
			points = append(points, matrix.MatrixPoint{
				X:     x,
				Y:     y + yShift,
				Color: rgbaAt(c.actual, x, y),
			})
		}
	}

	return points
}

func TestFillVerticalFrameMatchesReference(t *testing.T) {
	t.Parallel()

	l := zap.NewNop()
	for _, dims := range []struct{ w, h int }{{64, 32}, {128, 32}, {32, 16}} {
		c, err := NewScrollCanvas(matrix.NewConsoleMatrix(dims.w, dims.h, io.Discard, l), l,
			WithScrollDirection(BottomToTop))
		require.NoError(t, err)
		c.SetPadding(dims.h * 2)
		draw.Draw(c.actual, image.Rect(0, 0, dims.w, dims.h),
			testActual(0, dims.w, dims.h), image.Point{}, draw.Src)

		// Sweep the whole scroll, plus well past both ends so the clamped and
		// out-of-image edges are covered.
		for yShift := -(c.actual.Bounds().Max.Y + 4); yShift <= dims.h+4; yShift++ {
			buf := getLoader(c.w * c.h)
			got := c.fillVerticalFrame(buf, yShift)
			want := c.referenceFillVerticalFrame(yShift)

			require.Equal(t, len(want), len(got),
				"%dx%d yShift=%d: point count", dims.w, dims.h, yShift)
			for i := range want {
				require.Equal(t, want[i], got[i],
					"%dx%d yShift=%d: point %d", dims.w, dims.h, yShift, i)
			}
			putLoader(buf)
		}
	}
}
