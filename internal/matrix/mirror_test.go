package matrix

import (
	"context"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMirrorSnapshot(t *testing.T) {
	t.Parallel()

	m := NewMirror(3, 2)
	img, first := m.Snapshot()
	require.Equal(t, image.Rect(0, 0, 3, 2), img.Bounds())
	require.Equal(t, color.RGBA{0, 0, 0, 0xff}, img.RGBAAt(2, 1))

	frame := make([]uint32, 6)
	frame[1] = 0xff8000 // x 1, y 0
	frame[5] = 0x0000ff // x 2, y 1
	m.Capture(frame)

	img, tag := m.Snapshot()
	require.NotEqual(t, first, tag)
	require.Equal(t, tag, m.Tag())
	require.Equal(t, color.RGBA{0xff, 0x80, 0, 0xff}, img.RGBAAt(1, 0))
	require.Equal(t, color.RGBA{0, 0, 0xff, 0xff}, img.RGBAAt(2, 1))
	require.Equal(t, color.RGBA{0, 0, 0, 0xff}, img.RGBAAt(0, 0))
}

func TestMirrorWait(t *testing.T) {
	t.Parallel()

	m := NewMirror(2, 2)
	tag := m.Tag()

	// A frame from before a restart has been replaced already.
	require.True(t, m.Wait(context.Background(), `"0-0"`))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.False(t, m.Wait(ctx, tag))

	go func() {
		time.Sleep(20 * time.Millisecond)
		m.Capture(make([]uint32, 4))
	}()
	require.True(t, m.Wait(context.Background(), tag))
}

// Capture runs on every frame the panel shows, scroll frames included.
func TestMirrorCaptureDoesNotAllocate(t *testing.T) {
	m := NewMirror(64, 32)
	frame := make([]uint32, 64*32)

	require.Zero(t, testing.AllocsPerRun(100, func() { m.Capture(frame) }))
}
