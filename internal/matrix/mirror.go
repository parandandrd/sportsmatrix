package matrix

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// MaxWait is the longest a request can ask to be held for a new frame.
const MaxWait = 10 * time.Second

// Mirror keeps a copy of the frame a matrix is showing, so the web UI can show
// exactly what is on the panel. The drivers hand it every frame they show, the
// frames of a scroll included.
type Mirror struct {
	width  int
	height int
	// boot tells this process's frames from the last one's, so a browser
	// holding a frame from before a restart never matches a new one.
	boot int64

	lock  sync.Mutex
	frame []uint32
	seq   uint64
	// next is closed when the frame changes. It is only made while a request
	// is waiting for it, so a frame that nobody is watching allocates nothing.
	next chan struct{}
}

// Mirrored is a Matrix that keeps a Mirror of what it shows.
type Mirrored interface {
	Mirror() *Mirror
}

// NewMirror returns a Mirror of a width x height panel, showing black.
func NewMirror(width int, height int) *Mirror {
	return &Mirror{
		width:  width,
		height: height,
		boot:   time.Now().UnixNano(),
		frame:  make([]uint32, width*height),
	}
}

// Capture records the frame the panel is showing, packed 0x00RRGGBB the way
// the drivers pack it. It runs on every frame, so it copies into the buffer it
// already has.
func (m *Mirror) Capture(frame []uint32) {
	m.lock.Lock()
	defer m.lock.Unlock()

	copy(m.frame, frame)
	m.seq++

	if m.next != nil {
		close(m.next)
		m.next = nil
	}
}

// Tag names the frame showing now, as an HTTP entity tag.
func (m *Mirror) Tag() string {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.tagLocked()
}

func (m *Mirror) tagLocked() string {
	return fmt.Sprintf(`"%x-%d"`, m.boot, m.seq)
}

// Wait blocks until the panel is no longer showing the frame tagged tag, and
// reports whether it changed before ctx was done.
func (m *Mirror) Wait(ctx context.Context, tag string) bool {
	m.lock.Lock()
	if m.tagLocked() != tag {
		m.lock.Unlock()
		return true
	}
	if m.next == nil {
		m.next = make(chan struct{})
	}
	next := m.next
	m.lock.Unlock()

	select {
	case <-next:
		return true
	case <-ctx.Done():
		return false
	}
}

// Snapshot returns the frame showing now as an image, and its tag.
func (m *Mirror) Snapshot() (*image.RGBA, string) {
	img := image.NewRGBA(image.Rect(0, 0, m.width, m.height))

	m.lock.Lock()
	defer m.lock.Unlock()

	for i, px := range m.frame {
		p := img.Pix[i*4 : i*4+4 : i*4+4]
		p[0] = uint8(px >> 16)
		p[1] = uint8(px >> 8)
		p[2] = uint8(px)
		p[3] = 0xff
	}

	return img, m.tagLocked()
}

// ServeHTTP answers with the frame showing now, as a PNG. A request whose
// If-None-Match is that frame gets 304 Not Modified -- unless it also asks to
// wait, ?wait=10 for up to ten seconds, in which case it is held until the
// frame changes. A browser can follow the panel that way without polling it: a
// still board costs one request per wait, and a scroll arrives frame by frame
// as fast as the browser asks.
func (m *Mirror) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	if seen := req.Header.Get("If-None-Match"); seen != "" {
		changed := seen != m.Tag()
		if wait := frameWait(req); !changed && wait > 0 {
			ctx, cancel := context.WithTimeout(req.Context(), wait)
			changed = m.Wait(ctx, seen)
			cancel()
		}
		if !changed {
			w.Header().Set("ETag", seen)
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	img, tag := m.Snapshot()
	frame, err := encodeFrame(img)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	if wait := time.Duration(secs * float64(time.Second)); wait < MaxWait {
		return wait
	}

	return MaxWait
}

// encodeFrame favours speed over size, and reuses the compressor between
// frames rather than allocating a new one -- over half a megabyte -- for every
// frame.
func encodeFrame(img image.Image) ([]byte, error) {
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
