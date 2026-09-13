package rgbrender

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/robbydyer/sports/internal/board"
	"github.com/robbydyer/sports/internal/canvas"
	"github.com/robbydyer/sports/internal/matrix"
)

func overlappingLayer(clr color.Color, region image.Rectangle) *Layer {
	return NewLayer(
		func(ctx context.Context) (image.Image, error) {
			img := image.NewRGBA(region)
			draw.Draw(img, region, &image.Uniform{clr}, image.Point{}, draw.Src)

			return img, nil
		},
		func(c board.Canvas, img image.Image) error {
			draw.Draw(c, region, img, region.Min, draw.Over)

			return nil
		},
	)
}

// Layers at the same priority share a canvas, and draw.Over read-modify-writes
// every destination pixel, so drawing them concurrently races on any pixel two
// of them touch. Meaningful under -race.
func TestSamePriorityLayersDoNotRace(t *testing.T) {
	t.Parallel()

	cnvs := canvas.NewCanvas(matrix.NewConsoleMatrix(64, 32, nil, zap.NewNop()))

	ld, err := NewLayerDrawer(5*time.Second, zap.NewNop())
	require.NoError(t, err)

	region := image.Rect(0, 0, 64, 32)
	ld.AddLayer(BackgroundPriority, overlappingLayer(color.RGBA{R: 255, A: 255}, region))
	ld.AddLayer(BackgroundPriority, overlappingLayer(color.RGBA{G: 255, A: 255}, region))
	ld.AddTextLayer(BackgroundPriority, NewTextLayer(
		func(ctx context.Context) (*TextWriter, []string, error) {
			w, err := DefaultTextWriter()

			return w, []string{"3-2"}, err
		},
		func(c board.Canvas, w *TextWriter, txt []string) error {
			return w.WriteAligned(CenterCenter, c, region, txt, color.White)
		},
	))

	require.NoError(t, ld.Draw(context.Background(), cnvs))
}

// Layers must be drawn back to front, so a later priority wins the pixel.
func TestDrawRespectsPriorityOrder(t *testing.T) {
	t.Parallel()

	m := matrix.NewConsoleMatrix(8, 8, nil, zap.NewNop())
	cnvs := canvas.NewCanvas(m)

	ld, err := NewLayerDrawer(5*time.Second, zap.NewNop())
	require.NoError(t, err)

	region := image.Rect(0, 0, 8, 8)
	ld.AddLayer(BackgroundPriority+1, overlappingLayer(color.RGBA{G: 255, A: 255}, region))
	ld.AddLayer(BackgroundPriority, overlappingLayer(color.RGBA{R: 255, A: 255}, region))

	require.NoError(t, ld.Draw(context.Background(), cnvs))

	r, g, b, _ := cnvs.At(4, 4).RGBA()
	require.Equal(t, []uint32{0, 0xffff, 0}, []uint32{r, g, b},
		"the higher priority layer should be on top")
}

// A canceled context must stop drawing rather than press on.
func TestDrawStopsOnCanceledContext(t *testing.T) {
	t.Parallel()

	cnvs := canvas.NewCanvas(matrix.NewConsoleMatrix(8, 8, nil, zap.NewNop()))

	ld, err := NewLayerDrawer(5*time.Second, zap.NewNop())
	require.NoError(t, err)

	ld.AddLayer(BackgroundPriority, overlappingLayer(color.RGBA{R: 255, A: 255}, image.Rect(0, 0, 8, 8)))
	require.NoError(t, ld.Prepare(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.ErrorIs(t, ld.Draw(ctx, cnvs), context.Canceled)
}
