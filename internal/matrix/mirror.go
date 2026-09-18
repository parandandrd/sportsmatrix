package matrix

import (
	"context"
	"fmt"
	"image"
	"sync"
	"time"
)

// Mirror keeps a copy of the frame a matrix is showing, so the web board can
// show exactly what is on the panel rather than drawing every board a second
// time at its own size. The drivers hand it every frame they show, the frames
// of a scroll included.
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
