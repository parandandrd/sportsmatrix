package imgcanvas

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"sync"
	"time"

	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
)

// idleTimeout is how long the canvas goes on drawing after the last request for
// a frame. A browser following the board asks at least every
// board.MaxFrameWait, so this only runs out once nobody is watching -- a tab
// that was closed without saying so included.
const idleTimeout = 2 * board.MaxFrameWait

// ImgCanvas is a board.Canvas that keeps what is drawn on it as a PNG, for the
// web board's full-size view. Drawing every board a second time at this size is
// the most expensive thing the service does on a Pi 3, so it only draws while a
// browser is asking for frames.
type ImgCanvas struct {
	width   int
	height  int
	img     *image.RGBA
	enabled *atomic.Bool
	// lastSeen is when a browser last asked for a frame, in Unix nanoseconds.
	lastSeen *atomic.Int64
	idle     time.Duration
	log      *zap.Logger
	done     chan struct{}
	// boot tells this process's frames from the last one's, so a browser
	// holding a frame from before a restart never matches a new one.
	boot int64

	sync.Mutex
	lastPng []byte
	seq     uint64
	// next is closed when the frame changes, and only made while a request is
	// waiting for it.
	next chan struct{}
}

// New ...
func New(width int, height int, logger *zap.Logger) *ImgCanvas {
	return newCanvas(width, height, logger, idleTimeout)
}

func newCanvas(width int, height int, logger *zap.Logger, idle time.Duration) *ImgCanvas {
	i := &ImgCanvas{
		width:    width,
		height:   height,
		img:      image.NewRGBA(image.Rect(0, 0, width, height)),
		enabled:  atomic.NewBool(false),
		lastSeen: atomic.NewInt64(0),
		idle:     idle,
		log:      logger,
		done:     make(chan struct{}),
		boot:     time.Now().UnixNano(),
	}

	i.blackOut()

	go i.idleWatcher()

	return i
}

// Name ...
func (i *ImgCanvas) Name() string {
	return "ImgCanvas"
}

// Scrollable ...
func (i *ImgCanvas) Scrollable() bool {
	return false
}

// AlwaysRender ...
func (i *ImgCanvas) AlwaysRender() bool {
	return true
}

// Close ...
func (i *ImgCanvas) Close() error {
	i.done <- struct{}{}

	return nil
}

// SetWidth ...
func (i *ImgCanvas) SetWidth(x int) {}

// GetWidth ...
func (i *ImgCanvas) GetWidth() int {
	return i.width
}

// idleWatcher stops the canvas once nobody has asked for a frame in i.idle.
func (i *ImgCanvas) idleWatcher() {
	ticker := time.NewTicker(i.idle / 20)
	defer ticker.Stop()

	for {
		select {
		case <-i.done:
			return
		case <-ticker.C:
			if i.Enabled() && time.Since(time.Unix(0, i.lastSeen.Load())) > i.idle {
				i.log.Info("nobody is watching the full-size web board, no longer drawing it")
				i.Disable()
			}
		}
	}
}

// Clear sets the canvas to all black
func (i *ImgCanvas) Clear() error {
	i.blackOut()
	return i.Render(context.Background())
}

func (i *ImgCanvas) blackOut() {
	pix := i.img.Pix
	for p := 0; p < len(pix); p += 4 {
		pix[p], pix[p+1], pix[p+2], pix[p+3] = 0, 0, 0, 0xff
	}
}

// Render keeps what has been drawn as the next frame, while anyone is watching.
func (i *ImgCanvas) Render(ctx context.Context) error {
	defer i.blackOut()

	if !i.Enabled() {
		return nil
	}

	frame, err := board.EncodeFrame(i.img)
	if err != nil {
		return err
	}

	i.Lock()
	defer i.Unlock()

	// Stopped while encoding: a frame kept now would be out of date by the time
	// anyone asked for one again.
	if !i.Enabled() {
		return nil
	}
	i.lastPng = frame
	i.frameChangedLocked()

	return nil
}

func (i *ImgCanvas) frameChangedLocked() {
	i.seq++
	if i.next != nil {
		close(i.next)
		i.next = nil
	}
}

// FrameTag is the entity tag of the last frame, or "" when there is none.
func (i *ImgCanvas) FrameTag() string {
	i.Lock()
	defer i.Unlock()

	return i.frameTagLocked()
}

func (i *ImgCanvas) frameTagLocked() string {
	if i.lastPng == nil {
		return ""
	}

	return fmt.Sprintf(`"%x-%d"`, i.boot, i.seq)
}

// WaitFrame blocks until the frame tagged tag is replaced, and reports whether
// that happened before ctx was done.
func (i *ImgCanvas) WaitFrame(ctx context.Context, tag string) bool {
	i.Lock()
	if i.frameTagLocked() != tag {
		i.Unlock()
		return true
	}
	if i.next == nil {
		i.next = make(chan struct{})
	}
	next := i.next
	i.Unlock()

	select {
	case <-next:
		return true
	case <-ctx.Done():
		return false
	}
}

// FramePNG is the last frame and its tag, or nil until a board has drawn one.
// That is not straight away: a board draws to the canvases that were on when it
// started, so a canvas turned on part way through a board gets its first frame
// when the next board starts.
func (i *ImgCanvas) FramePNG() ([]byte, string, error) {
	i.Lock()
	defer i.Unlock()

	return i.lastPng, i.frameTagLocked(), nil
}

// ColorModel returns the canvas' color model, always color.RGBAModel
func (i *ImgCanvas) ColorModel() color.Model {
	return color.RGBAModel
}

// Bounds return the topology of the Canvas
func (i *ImgCanvas) Bounds() image.Rectangle {
	return image.Rect(0, 0, i.width, i.height)
}

// At returns the color of the pixel at (x, y)
func (i *ImgCanvas) At(x, y int) color.Color {
	if !(image.Point{x, y}.In(i.img.Rect)) {
		return color.Black
	}

	return i.img.RGBAAt(x, y)
}

// Set colors the pixel at x,y. While nobody is watching it does nothing, so a
// board that is part way through drawing when the last browser leaves costs
// little for the rest of it.
func (i *ImgCanvas) Set(x, y int, clr color.Color) {
	if !i.Enabled() {
		return
	}

	var c color.RGBA
	if clr != nil {
		// A color's RGBA method returns values in the range [0, 65535]
		r, g, b, _ := clr.RGBA()
		c = color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0}
	}
	c.A = 0xff

	i.img.SetRGBA(x, y, c)
}

// Enabled ...
func (i *ImgCanvas) Enabled() bool {
	return i.enabled.Load()
}

// Enable turns the canvas on, and counts as someone watching it.
func (i *ImgCanvas) Enable() bool {
	i.lastSeen.Store(time.Now().UnixNano())

	return i.enabled.CompareAndSwap(false, true)
}

func (i *ImgCanvas) SetStateChangeCallback(s func()) {
}

// Disable turns the canvas off and drops its frame, which would be out of date
// by the time anyone asked for it again. Anyone waiting for the next frame is
// told there is none.
func (i *ImgCanvas) Disable() bool {
	if !i.enabled.CompareAndSwap(true, false) {
		return false
	}

	i.Lock()
	defer i.Unlock()

	i.lastPng = nil
	i.frameChangedLocked()

	return true
}

func (i *ImgCanvas) Store(s bool) bool {
	if s {
		return i.Enable()
	}

	return i.Disable()
}
