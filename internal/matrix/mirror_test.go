package matrix

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
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
// nolint: paralleltest // AllocsPerRun counts allocations across the whole process
func TestMirrorCaptureDoesNotAllocate(t *testing.T) {
	m := NewMirror(64, 32)
	frame := make([]uint32, 64*32)

	require.Zero(t, testing.AllocsPerRun(100, func() { m.Capture(frame) }))
}

func getFrame(m *Mirror, query string, seen string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/panel/frame"+query, nil)
	if seen != "" {
		req.Header.Set("If-None-Match", seen)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)

	return rec
}

func TestMirrorServeHTTP(t *testing.T) {
	t.Parallel()

	m := NewMirror(4, 2)

	rec := getFrame(m, "", "")
	require.Equal(t, http.StatusOK, rec.Code, "black until the first frame, but always a frame")
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	first := rec.Header().Get("ETag")
	require.Equal(t, m.Tag(), first)

	rec = getFrame(m, "", first)
	require.Equal(t, http.StatusNotModified, rec.Code, "has it already, and did not ask to wait")
	require.Empty(t, rec.Body.String())

	frame := make([]uint32, 8)
	frame[7] = 0x00ff00 // x 3, y 1
	m.Capture(frame)

	rec = getFrame(m, "", first)
	require.Equal(t, http.StatusOK, rec.Code, "has an older one")
	require.NotEqual(t, first, rec.Header().Get("ETag"))

	img, err := png.Decode(rec.Body)
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 4, 2), img.Bounds())
	r, g, b, _ := img.At(3, 1).RGBA()
	require.Equal(t, []uint32{0, 0xffff, 0}, []uint32{r, g, b})
}

func TestMirrorServeHTTPWaitsForTheNext(t *testing.T) {
	t.Parallel()

	m := NewMirror(2, 2)
	tag := m.Tag()

	go func() {
		time.Sleep(50 * time.Millisecond)
		m.Capture(make([]uint32, 4))
	}()
	began := time.Now()
	rec := getFrame(m, "?wait=5", tag)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotEqual(t, tag, rec.Header().Get("ETag"))
	require.Less(t, time.Since(began), 2*time.Second, "answered when the frame changed, not at the end of the wait")

	tag = rec.Header().Get("ETag")
	began = time.Now()
	rec = getFrame(m, "?wait=0.1", tag)
	require.Equal(t, http.StatusNotModified, rec.Code, "nothing changed in the wait")
	require.GreaterOrEqual(t, time.Since(began), 90*time.Millisecond)
}

func TestFrameWait(t *testing.T) {
	t.Parallel()

	for query, want := range map[string]time.Duration{
		"":            0,
		"?wait=soon":  0,
		"?wait=-1":    0,
		"?wait=0.5":   500 * time.Millisecond,
		"?wait=10":    MaxWait,
		"?wait=86400": MaxWait,
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/panel/frame"+query, nil)
		require.Equal(t, want, frameWait(req), query)
	}
}

func TestEncodeFrame(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.SetRGBA(3, 1, color.RGBA{10, 20, 30, 0xff})

	// Each encode reuses a pooled compressor, which must not leak one frame
	// into the next.
	for n := 0; n < 3; n++ {
		frame, err := encodeFrame(img)
		require.NoError(t, err)

		got, err := png.Decode(bytes.NewReader(frame))
		require.NoError(t, err)
		require.Equal(t, img.Bounds(), got.Bounds())
		r, g, b, _ := got.At(3, 1).RGBA()
		require.Equal(t, []uint32{10, 20, 30}, []uint32{r >> 8, g >> 8, b >> 8})
	}
}
