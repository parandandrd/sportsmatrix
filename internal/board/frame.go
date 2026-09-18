package board

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// FrameSource is a picture of what is showing that the web UI follows: the
// panel's own frames, or the web board's canvas.
type FrameSource interface {
	// FrameTag is the entity tag of the frame showing now, or "" when there
	// is none yet.
	FrameTag() string
	// WaitFrame blocks until the frame tagged tag is replaced, and reports
	// whether that happened before ctx was done.
	WaitFrame(ctx context.Context, tag string) bool
	// FramePNG is the frame showing now and its tag, or nil when there is
	// none yet.
	FramePNG() ([]byte, string, error)
}

// MaxFrameWait is the longest a request can ask to be held for a new frame.
const MaxFrameWait = 10 * time.Second

// ServeFrame answers a request for src's frame. A request whose If-None-Match
// is the frame showing now gets 304 Not Modified -- unless it also asks to wait,
// ?wait=10 for up to ten seconds, in which case it is held until the frame
// changes. A browser can follow the panel that way without polling it: a still
// board costs one request per wait, and a scroll arrives frame by frame as fast
// as the browser asks. With no frame yet the answer is 204 No Content.
func ServeFrame(w http.ResponseWriter, req *http.Request, src FrameSource) {
	w.Header().Set("Cache-Control", "no-store")

	if seen := req.Header.Get("If-None-Match"); seen != "" {
		changed := seen != src.FrameTag()
		if wait := frameWait(req); !changed && wait > 0 {
			ctx, cancel := context.WithTimeout(req.Context(), wait)
			changed = src.WaitFrame(ctx, seen)
			cancel()
		}
		if !changed {
			w.Header().Set("ETag", seen)
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	frame, tag, err := src.FramePNG()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if frame == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("ETag", tag)
	_, _ = w.Write(frame)
}

func frameWait(req *http.Request) time.Duration {
	secs, err := strconv.ParseFloat(req.URL.Query().Get("wait"), 64)
	if err != nil || secs <= 0 {
		return 0
	}
	if wait := time.Duration(secs * float64(time.Second)); wait < MaxFrameWait {
		return wait
	}

	return MaxFrameWait
}

// EncodeFrame encodes a frame for the web UI. It favours speed over size, and
// reuses the compressor between frames rather than allocating a new one -- over
// half a megabyte -- for every frame.
func EncodeFrame(img image.Image) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := frameEncoder.Encode(buf, img); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

var frameEncoder = &png.Encoder{
	CompressionLevel: png.BestSpeed,
	BufferPool:       &encoderBuffers{},
}

type encoderBuffers struct {
	pool sync.Pool
}

func (e *encoderBuffers) Get() *png.EncoderBuffer {
	b, _ := e.pool.Get().(*png.EncoderBuffer)
	return b
}

func (e *encoderBuffers) Put(b *png.EncoderBuffer) {
	e.pool.Put(b)
}
