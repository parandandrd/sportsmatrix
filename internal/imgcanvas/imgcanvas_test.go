package imgcanvas

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
	"go.uber.org/zap"
)

func newTestCanvas(t *testing.T, idle time.Duration) *ImgCanvas {
	i := newCanvas(8, 4, zap.NewNop(), idle)
	t.Cleanup(func() { _ = i.Close() })

	return i
}

func TestKeepsFramesOnlyWhileWatched(t *testing.T) {
	t.Parallel()

	i := newTestCanvas(t, time.Hour)
	black := color.RGBA{0, 0, 0, 0xff}

	i.Set(1, 1, color.White)
	require.Equal(t, black, i.img.RGBAAt(1, 1), "drawing does nothing while nobody watches")
	require.NoError(t, i.Render(context.Background()))
	frame, tag, err := i.FramePNG()
	require.NoError(t, err)
	require.Nil(t, frame)
	require.Empty(t, tag)

	require.True(t, i.Enable())
	i.Set(1, 1, color.White)
	require.NoError(t, i.Render(context.Background()))
	frame, tag, err = i.FramePNG()
	require.NoError(t, err)
	require.NotEmpty(t, tag)

	img, err := png.Decode(bytes.NewReader(frame))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 8, 4), img.Bounds())
	r, g, b, _ := img.At(1, 1).RGBA()
	require.Equal(t, []uint32{0xffff, 0xffff, 0xffff}, []uint32{r, g, b})
	r, g, b, _ = img.At(0, 0).RGBA()
	require.Equal(t, []uint32{0, 0, 0}, []uint32{r, g, b})
	require.Equal(t, black, i.img.RGBAAt(1, 1), "the next frame starts from black")

	require.True(t, i.Disable())
	frame, tag, err = i.FramePNG()
	require.NoError(t, err)
	require.Nil(t, frame, "turning it off drops the frame, which would be stale by the next look")
	require.Empty(t, tag)
}

func TestStopsWhenNobodyAsks(t *testing.T) {
	t.Parallel()

	i := newTestCanvas(t, 100*time.Millisecond)
	require.True(t, i.Enable())
	require.NoError(t, i.Render(context.Background()))

	require.Eventually(t, func() bool { return !i.Enabled() }, 2*time.Second, 10*time.Millisecond)
	frame, _, err := i.FramePNG()
	require.NoError(t, err)
	require.Nil(t, frame)
}

func TestKeepsDrawingWhileAsked(t *testing.T) {
	t.Parallel()

	i := newTestCanvas(t, 200*time.Millisecond)
	require.True(t, i.Enable())
	for n := 0; n < 8; n++ {
		time.Sleep(50 * time.Millisecond)
		i.Enable()
	}
	require.True(t, i.Enabled())
}

func TestBoardHandler(t *testing.T) {
	t.Parallel()

	i := newTestCanvas(t, time.Hour)
	handlers, err := i.GetHTTPHandlers()
	require.NoError(t, err)
	var serve func(http.ResponseWriter, *http.Request)
	for _, h := range handlers {
		if h.Path == "/api/imgcanvas/board" {
			serve = h.Handler
		}
	}
	require.NotNil(t, serve)

	get := func(query string, seen string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/imgcanvas/board"+query, nil)
		if seen != "" {
			req.Header.Set("If-None-Match", seen)
		}
		rec := httptest.NewRecorder()
		serve(rec, req)
		return rec
	}

	rec := get("", "")
	require.Equal(t, http.StatusNoContent, rec.Code, "asking turns it on, but no board has drawn yet")
	require.True(t, i.Enabled())

	require.NoError(t, i.Render(context.Background()))
	rec = get("", "")
	require.Equal(t, http.StatusOK, rec.Code)
	tag := rec.Header().Get("ETag")
	require.NotEmpty(t, tag)

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = i.Render(context.Background())
	}()
	rec = get("?wait=5", tag)
	require.Equal(t, http.StatusOK, rec.Code, "held until the next frame")
	require.NotEqual(t, tag, rec.Header().Get("ETag"))

	tag = rec.Header().Get("ETag")
	go func() {
		time.Sleep(50 * time.Millisecond)
		i.Disable()
	}()
	rec = get("?wait=5", tag)
	require.Equal(t, http.StatusNoContent, rec.Code, "turned off while waiting: there is no frame now")
}
