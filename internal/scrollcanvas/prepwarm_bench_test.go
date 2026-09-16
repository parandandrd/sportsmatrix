package scrollcanvas

import (
	"context"
	"image"
	"image/draw"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/matrix"
)

// BenchmarkVerticalPrepWarm reuses one canvas and matrix across scrolls, which
// is how the real thing runs: a board scrolls over and over against the same
// Matrix. BenchmarkVerticalPrep builds a fresh matrix per iteration and so only
// ever measures a cold first scroll.
func BenchmarkVerticalPrepWarm(b *testing.B) {
	ctx := context.Background()
	l := zap.NewNop()

	c, err := NewScrollCanvas(matrix.NewConsoleMatrix(128, 32, io.Discard, l), l,
		WithScrollDirection(BottomToTop))
	require.NoError(b, err)
	c.SetPadding(160)
	draw.Draw(c.actual, image.Rect(0, 0, 128, 32), testActual(0, 128, 32), image.Point{}, draw.Src)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.verticalPrep(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
