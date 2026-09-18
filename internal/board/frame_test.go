package board

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeFrames struct {
	lock  sync.Mutex
	frame []byte
	tag   string
	next  chan struct{}
}

func newFakeFrames() *fakeFrames {
	return &fakeFrames{next: make(chan struct{})}
}

func (f *fakeFrames) show(frame string, tag string) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.frame, f.tag = []byte(frame), tag
	close(f.next)
	f.next = make(chan struct{})
}

func (f *fakeFrames) FrameTag() string {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.tag
}

func (f *fakeFrames) WaitFrame(ctx context.Context, tag string) bool {
	f.lock.Lock()
	if f.tag != tag {
		f.lock.Unlock()
		return true
	}
	next := f.next
	f.lock.Unlock()

	select {
	case <-next:
		return true
	case <-ctx.Done():
		return false
	}
}

func (f *fakeFrames) FramePNG() ([]byte, string, error) {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.frame, f.tag, nil
}

func getFrame(src FrameSource, query string, seen string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/frame"+query, nil)
	if seen != "" {
		req.Header.Set("If-None-Match", seen)
	}
	rec := httptest.NewRecorder()
	ServeFrame(rec, req, src)

	return rec
}

func TestServeFrame(t *testing.T) {
	t.Parallel()

	src := newFakeFrames()

	rec := getFrame(src, "", "")
	require.Equal(t, http.StatusNoContent, rec.Code, "nothing drawn yet")

	src.show("one", `"a-1"`)
	rec = getFrame(src, "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "one", rec.Body.String())
	require.Equal(t, `"a-1"`, rec.Header().Get("ETag"))
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	rec = getFrame(src, "", `"a-1"`)
	require.Equal(t, http.StatusNotModified, rec.Code, "has it already, and did not ask to wait")
	require.Empty(t, rec.Body.String())

	src.show("two", `"a-2"`)
	rec = getFrame(src, "", `"a-1"`)
	require.Equal(t, http.StatusOK, rec.Code, "has an older one")
	require.Equal(t, "two", rec.Body.String())
}

func TestServeFrameWaitsForTheNext(t *testing.T) {
	t.Parallel()

	src := newFakeFrames()
	src.show("one", `"a-1"`)

	go func() {
		time.Sleep(50 * time.Millisecond)
		src.show("two", `"a-2"`)
	}()
	began := time.Now()
	rec := getFrame(src, "?wait=5", `"a-1"`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "two", rec.Body.String())
	require.Less(t, time.Since(began), 2*time.Second, "answered when the frame changed, not at the end of the wait")

	began = time.Now()
	rec = getFrame(src, "?wait=0.1", `"a-2"`)
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
		"?wait=10":    MaxFrameWait,
		"?wait=86400": MaxFrameWait,
	} {
		req := httptest.NewRequest(http.MethodGet, "/frame"+query, nil)
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
		frame, err := EncodeFrame(img)
		require.NoError(t, err)

		got, err := png.Decode(bytes.NewReader(frame))
		require.NoError(t, err)
		require.Equal(t, img.Bounds(), got.Bounds())
		r, g, b, _ := got.At(3, 1).RGBA()
		require.Equal(t, []uint32{10, 20, 30}, []uint32{r >> 8, g >> 8, b >> 8})
	}
}
