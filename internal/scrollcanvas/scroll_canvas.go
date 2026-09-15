package scrollcanvas

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/matrix"
)

var (
	black = color.RGBA{R: 0x0, G: 0x0, B: 0x0, A: 0x0}
	// opaqueBlack is the RGBA equivalent of color.Black, which is what a
	// scroll falls back to when no subcanvas covers a coordinate.
	opaqueBlack = color.RGBA{A: 0xff}
	// transparent is what (*image.RGBA).At returns outside its bounds.
	transparent        = color.RGBA{}
	DefaultScrollDelay = 50 * time.Millisecond
)

// ScrollDirection represents the direction the canvas scrolls
type ScrollDirection int

const (
	// RightToLeft ...
	RightToLeft ScrollDirection = iota
	// LeftToRight ...
	LeftToRight
	// BottomToTop ...
	BottomToTop
	// TopToBottom ...
	TopToBottom
)

type ScrollCanvas struct {
	name                string
	w, h                int
	Matrix              matrix.Matrix
	enabled             *atomic.Bool
	preloadThreads      int
	actual              *image.RGBA
	direction           ScrollDirection
	interval            *atomic.Duration
	log                 *zap.Logger
	pad                 int
	actuals             []*image.RGBA
	merged              *atomic.Bool
	subCanvases         []*subCanvasHorizontal
	mergePad            int
	scrollStatus        chan float64
	stateChangeCallback func()
	sendScrollSpeedChan chan time.Duration
	speedLock           sync.Mutex
	matchScrollCtx      context.Context
	matchScrollCancel   context.CancelFunc
}

type subCanvasHorizontal struct {
	actualStartX  int
	actualEndX    int
	virtualStartX int
	virtualEndX   int
	img           *image.RGBA
	previous      *subCanvasHorizontal
}

type ScrollCanvasOption func(*ScrollCanvas) error

// preloadSpan is a run of matrix columns that all come from one subcanvas at a
// constant source offset, so the whole run can be copied without re-resolving
// which subcanvas each column belongs to. A nil img means the run is off the
// end of the content and should be filled with black.
type preloadSpan struct {
	img     *image.RGBA
	xStart  int
	xEnd    int
	actualX int
}

// preloadBuf is the scratch a single preload frame needs.
type preloadBuf struct {
	points []matrix.MatrixPoint
	spans  []preloadSpan
}

// loaderPool recycles preload scratch. A scroll builds one buffer per frame and
// hands it straight to Matrix.PreLoad, which copies out of it synchronously, so
// the buffer can go right back in the pool. Without this, a single scroll of a
// few boards churns hundreds of megabytes -- which matters a lot on a Pi.
var loaderPool sync.Pool

func getLoader(size int) *preloadBuf {
	buf, ok := loaderPool.Get().(*preloadBuf)
	if !ok {
		buf = &preloadBuf{}
	}

	if cap(buf.points) >= size {
		buf.points = buf.points[:size]
	} else {
		buf.points = make([]matrix.MatrixPoint, size)
	}
	buf.spans = buf.spans[:0]

	return buf
}

func putLoader(buf *preloadBuf) {
	loaderPool.Put(buf)
}

func NewScrollCanvas(m matrix.Matrix, logger *zap.Logger, opts ...ScrollCanvasOption) (*ScrollCanvas, error) {
	w, h := m.Geometry()
	c := &ScrollCanvas{
		w:         w,
		h:         h,
		Matrix:    m,
		enabled:   atomic.NewBool(true),
		interval:  atomic.NewDuration(DefaultScrollDelay),
		log:       logger,
		direction: RightToLeft,
		merged:    atomic.NewBool(false),
		pad:       w + int(float64(w)*0.25),
	}

	for _, f := range opts {
		if err := f(c); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func (c *ScrollCanvas) Width() int {
	return c.w
}

func (c *ScrollCanvas) SetWidth(w int) {
	c.w = w
	c.SetPadding(w + int(float64(w)*0.25))
}

func (c *ScrollCanvas) GetWidth() int {
	return c.w
}

func (c *ScrollCanvas) GetActual() *image.RGBA {
	return c.actual
}

// GC clears out the underlying struct fields that hold image data.
// This should be called whenever a ScrollCanvas is used that is not Rendered
// at some point or after Rendering
func (c *ScrollCanvas) GC() {
	for i := range c.subCanvases {
		c.subCanvases[i] = nil
	}
	for i := range c.actuals {
		c.actuals[i] = nil
	}
	c.subCanvases = nil
	c.actuals = nil
	c.actual = nil
}

func (c *ScrollCanvas) AddCanvas(add draw.Image) {
	if c.direction != RightToLeft && c.direction != LeftToRight {
		return
	}

	img := image.NewRGBA(add.Bounds())
	draw.Draw(img, add.Bounds(), add, add.Bounds().Min, draw.Over)

	c.actuals = append(c.actuals, img)
}

// Append the actual canvases of another ScrollCanvas to this one
func (c *ScrollCanvas) Append(other *ScrollCanvas) {
	c.actuals = append(c.actuals, other.actuals...)
}

// Append the actual canvases of another ScrollCanvas to this one
func (c *ScrollCanvas) AppendAndGC(other *ScrollCanvas) {
	c.actuals = append(c.actuals, other.actuals...)
	other.GC()
}

// Len returns the number of canvases
func (c *ScrollCanvas) Len() int {
	return len(c.actuals)
}

func (c *ScrollCanvas) Scrollable() bool {
	return true
}

func (c *ScrollCanvas) Name() string {
	return "RGB ScrollCanvas"
}

func (c *ScrollCanvas) AlwaysRender() bool {
	return false
}

// SetScrollSpeed ...
func (c *ScrollCanvas) SetScrollSpeed(d time.Duration) {
	// Even though updating interval is atomic, we still need the lock
	// to notify the channel
	c.speedLock.Lock()
	defer c.speedLock.Unlock()

	if !c.interval.CompareAndSwap(c.interval.Load(), d) {
		// no change
		return
	}

	if c.sendScrollSpeedChan == nil {
		return
	}

	maxTry := 2
	try := 0
	for {
		if try >= maxTry {
			break
		}
		try++
		select {
		case c.sendScrollSpeedChan <- c.interval.Load():
			c.log.Info("scroll canvas sending new speed to channel",
				zap.String("name", c.name),
				zap.Duration("speed", c.interval.Load()),
			)
			return
		default:
			c.log.Info("failed to send scroll canvas sending new speed to channel",
				zap.String("name", c.name),
				zap.Duration("speed", c.interval.Load()),
			)
			// Clear the buffer
			for i := 0; i < cap(c.sendScrollSpeedChan); i++ {
				select {
				case <-c.sendScrollSpeedChan:
					c.log.Info("cleared canvas speed channel buffer",
						zap.String("name", c.name),
						zap.Int("index", i),
					)
				default:
				}
			}
		}
	}
}

// GetScrollSpeed ...
func (c *ScrollCanvas) GetScrollSpeed() time.Duration {
	return c.interval.Load()
}

// SetScrollDirection ...
func (c *ScrollCanvas) SetScrollDirection(d ScrollDirection) {
	c.direction = d
}

// GetScrollDirection ...
func (c *ScrollCanvas) GetScrollDirection() ScrollDirection {
	return c.direction
}

// SetPadding ...
func (c *ScrollCanvas) SetPadding(pad int) {
	c.pad = pad

	c.actual = image.NewRGBA(c.getBounds())
	draw.Draw(c.actual, c.actual.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Over)

	c.log.Debug("creating scroll canvas",
		zap.Int("padding", c.pad),
		zap.Int("width", c.w),
		zap.Int("height", c.h),
		zap.Int("min X", c.Bounds().Min.X),
		zap.Int("min Y", c.Bounds().Min.Y),
		zap.Int("max X", c.Bounds().Max.X),
		zap.Int("max Y", c.Bounds().Max.Y),
	)
}

func (c *ScrollCanvas) getBounds() image.Rectangle {
	return image.Rect(0-c.pad, 0-c.pad, c.w+c.pad, c.h+c.pad)
}

// GetPadding
func (c *ScrollCanvas) GetPadding() int {
	return c.pad
}

// Clear set all the leds on the matrix with color.Black
func (c *ScrollCanvas) Clear() error {
	if c.actual == nil {
		c.SetPadding(c.pad)
	}
	draw.Draw(c.actual, c.actual.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Over)
	for x := 0; x < c.w-1; x++ {
		for y := 0; y < c.h-1; y++ {
			c.Matrix.Set(x, y, color.Black)
		}
	}
	return c.Matrix.Render()
}

// Close clears the matrix and close the matrix
func (c *ScrollCanvas) Close() error {
	_ = c.Clear()
	return c.Matrix.Close()
}

// Render update the display with the data from the LED buffer
func (c *ScrollCanvas) Render(ctx context.Context) error {
	defer func() {
		if c.matchScrollCancel != nil {
			c.matchScrollCancel()
			c.log.Info("scroll canvas cancel MatchScroll")
		}

		// Make sure to nil out these to ensure we don't leak memory
		c.GC()
	}()
	switch c.direction {
	case RightToLeft:
		c.log.Debug("scrolling right to left")
		if err := c.rightToLeft(ctx); err != nil {
			return err
		}
	case LeftToRight:
		c.log.Debug("scrolling left to right")
		if err := c.leftToRight(ctx); err != nil {
			return err
		}
	case BottomToTop:
		c.log.Debug("scrolling bottom to top")
		if err := c.bottomToTop(ctx); err != nil {
			return err
		}
	case TopToBottom:
		if err := c.topToBottom(ctx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported scroll direction")
	}

	return nil
}

// RenderWithStatus update the display with the data from the LED buffer
func (c *ScrollCanvas) RenderWithStatus(ctx context.Context, status chan float64) error {
	c.scrollStatus = status

	return c.Render(ctx)
}

// ColorModel returns the canvas' color model, always color.RGBAModel
func (c *ScrollCanvas) ColorModel() color.Model {
	return color.RGBAModel
}

// Bounds return the topology of the Canvas
func (c *ScrollCanvas) Bounds() image.Rectangle {
	if c.actual == nil {
		return c.getBounds()
	}
	return c.actual.Bounds()
}

// At returns the color of the pixel at (x, y)
func (c *ScrollCanvas) At(x int, y int) color.Color {
	if c.actual == nil {
		c.SetPadding(c.pad)
	}
	return c.actual.At(x, y)
}

// Set set LED at position x,y to the provided 24-bit color value
func (c *ScrollCanvas) Set(x int, y int, color color.Color) {
	if c.actual == nil {
		c.SetPadding(c.pad)
	}
	c.actual.Set(x, y, color)
}

// Enabled ...
func (c *ScrollCanvas) Enabled() bool {
	return c.enabled.Load()
}

// Enable ...
func (c *ScrollCanvas) Enable() bool {
	return c.enabled.CompareAndSwap(false, true)
}

// Disable ...
func (c *ScrollCanvas) Disable() bool {
	return c.enabled.CompareAndSwap(true, false)
}

func (c *ScrollCanvas) SetStateChangeCallback(s func()) {
	c.stateChangeCallback = s
}

func (c *ScrollCanvas) Store(s bool) bool {
	return c.enabled.CompareAndSwap(!s, s)
}

// GetHTTPHandlers ...
func (c *ScrollCanvas) GetHTTPHandlers() ([]*board.HTTPHandler, error) {
	return nil, nil
}

func (c *ScrollCanvas) topToBottom(ctx context.Context) error {
	if err := c.verticalPrep(ctx); err != nil {
		return err
	}

	// Default is bottomToTop, so reverse it
	c.Matrix.ReversePreLoad()

	c.sendScrollSpeedChan = make(chan time.Duration, 1)
	return c.Matrix.Play(ctx, c.GetScrollSpeed(), c.sendScrollSpeedChan)
}

func (c *ScrollCanvas) bottomToTop(ctx context.Context) error {
	if err := c.verticalPrep(ctx); err != nil {
		return err
	}

	c.sendScrollSpeedChan = make(chan time.Duration, 1)
	return c.Matrix.Play(ctx, c.GetScrollSpeed(), c.sendScrollSpeedChan)
}

func (c *ScrollCanvas) verticalPrep(ctx context.Context) error {
	thisY := firstNonBlankY(c.actual) + c.h
	finish := (lastNonBlankY(c.actual) + 1) * -1
	c.log.Debug("scrolling until line",
		zap.Int("finish line", finish),
		zap.Int("last Y index", c.actual.Bounds().Max.Y),
		zap.Int("thisY", thisY),
	)
	sceneIndex := 0
	wg, _ := errgroup.WithContext(ctx)
OUTER:
	for {
		if thisY == finish {
			break OUTER
		}

		mySceneIndex, myThisY := sceneIndex, thisY

		wg.Go(func() error {
			buf := getLoader(c.w * c.h)
			defer putLoader(buf)

			points := c.fillVerticalFrame(buf, myThisY)

			c.Matrix.PreLoad(&matrix.MatrixScene{
				Index:  mySceneIndex,
				Points: points,
			})
			return nil
		})
		sceneIndex++
		thisY--
	}

	return wg.Wait()
}

// frameSpans splits the matrix columns of one scroll frame into runs that each
// come from a single subcanvas.
//
// Which subcanvas a column belongs to depends only on x, so resolving it per
// pixel -- as this used to -- repeats the same lookup once per row. Worse, the
// subcanvases are contiguous and the frame walks them in order, so a cursor
// that only moves forward resolves the whole frame in one pass with branches a
// simple in-order predictor gets right every time. That matters on the Cortex-A53
// this runs on, which has no out-of-order execution to hide a mispredict.
func (c *ScrollCanvas) frameSpans(buf *preloadBuf, virtualXStart int) []preloadSpan {
	spans := buf.spans[:0]

	i := 0
	for x := 0; x < c.w; x++ {
		virtualX := virtualXStart + x

		for i < len(c.subCanvases) && c.subCanvases[i] != nil && c.subCanvases[i].virtualEndX < virtualX {
			i++
		}

		var img *image.RGBA
		actualX := 0
		if i < len(c.subCanvases) {
			if sub := c.subCanvases[i]; sub != nil && virtualX >= sub.virtualStartX {
				img = sub.img
				actualX = (virtualX - sub.virtualStartX) + sub.actualStartX
			}
		}

		// Extend the run in progress when this column continues it.
		if n := len(spans); n > 0 {
			if last := &spans[n-1]; last.img == img &&
				(img == nil || last.actualX+(x-last.xStart) == actualX) {
				last.xEnd = x + 1
				continue
			}
		}

		spans = append(spans, preloadSpan{img: img, xStart: x, xEnd: x + 1, actualX: actualX})
	}

	buf.spans = spans

	return spans
}

// fillHorizontalFrame writes one scroll frame into buf.points.
//
// Rows are the outer loop so that both the source pixels and the destination
// points are walked in address order. Going down a column instead steps through
// the source image one stride at a time -- 512 bytes apart on a 128-wide canvas,
// so a separate cache line every pixel, which the A53's stride prefetcher can't
// help with.
func (c *ScrollCanvas) fillHorizontalFrame(buf *preloadBuf, virtualXStart int) {
	spans := c.frameSpans(buf, virtualXStart)
	points := buf.points

	for _, span := range spans {
		for y := 0; y < c.h; y++ {
			row := y * c.w

			if span.img == nil {
				for x := span.xStart; x < span.xEnd; x++ {
					points[row+x] = matrix.MatrixPoint{X: x, Y: y, Color: opaqueBlack}
				}

				continue
			}

			for x := span.xStart; x < span.xEnd; x++ {
				points[row+x] = matrix.MatrixPoint{
					X:     x,
					Y:     y,
					Color: rgbaAt(span.img, span.actualX+(x-span.xStart), y),
				}
			}
		}
	}
}

// fillVerticalFrame writes one vertical scroll frame into buf.points and returns
// the points actually used.
//
// Only a window of the padded canvas can land on the matrix for a given offset,
// so the bounds are computed up front. Scanning the whole padded canvas and
// testing every pixel -- as this used to -- costs about 40x more iterations than
// it produces points on a 128x32 board.
func (c *ScrollCanvas) fillVerticalFrame(buf *preloadBuf, yShift int) []matrix.MatrixPoint {
	bounds := c.actual.Bounds()
	points := buf.points

	// Keep only what satisfies the original filter:
	// 0 < x < c.w and 0 < y+yShift < c.h.
	xLo := maxInt(bounds.Min.X, 1)
	xHi := minInt(bounds.Max.X, c.w-1)
	yLo := maxInt(bounds.Min.Y, 1-yShift)
	yHi := minInt(bounds.Max.Y, c.h-1-yShift)

	index := 0
	for y := yLo; y <= yHi; y++ {
		shiftY := y + yShift
		for x := xLo; x <= xHi; x++ {
			points[index] = matrix.MatrixPoint{
				X:     x,
				Y:     shiftY,
				Color: rgbaAt(c.actual, x, y),
			}
			index++
		}
	}

	return points[:index]
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}

	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}

	return b
}

// rgbaAt is (*image.RGBA).At without the boxing into a color.Color interface,
// which is a heap allocation per pixel in the preload loop. It matches At's
// behavior outside the image bounds, returning the zero (transparent) pixel.
func rgbaAt(img *image.RGBA, x int, y int) color.RGBA {
	if !(image.Point{X: x, Y: y}.In(img.Rect)) {
		return transparent
	}
	i := img.PixOffset(x, y)
	pix := img.Pix[i : i+4 : i+4]

	return color.RGBA{R: pix[0], G: pix[1], B: pix[2], A: pix[3]}
}

// PrepareSubCanvases
func (c *ScrollCanvas) PrepareSubCanvases() {
	if len(c.actuals) < 1 {
		c.actuals = append(c.actuals, c.actual)
	}

	c.log.Debug("preparing sub canvases",
		zap.Int("num actuals", len(c.actuals)),
	)

	// Built with append rather than a fixed-size slice with an index: a nil
	// entry in c.actuals used to leave trailing nils in here, which made the
	// last element nil and aborted horizontalPrep outright.
	c.subCanvases = make([]*subCanvasHorizontal, 0, (len(c.actuals)*2)+1)

	add := func(sub *subCanvasHorizontal) {
		if len(c.subCanvases) > 0 {
			sub.previous = c.subCanvases[len(c.subCanvases)-1]
		}
		c.subCanvases = append(c.subCanvases, sub)
	}

	// Add a matrix-width empty subcanvas so that we start
	// scrolling with a totally blank screen
	add(&subCanvasHorizontal{
		actualStartX:  0,
		actualEndX:    c.w,
		virtualStartX: 0,
		virtualEndX:   c.w,
		img:           image.NewRGBA(image.Rect(0, 0, c.w, c.h)),
		previous:      nil,
	})

ACTUALS:
	for i, actual := range c.actuals {
		if actual == nil {
			continue ACTUALS
		}
		// Add the actual subcanvas
		first, last := nonBlankXRange(actual)
		add(&subCanvasHorizontal{
			actualStartX: first,
			actualEndX:   last,
			img:          actual,
		})

		if i != len(c.actuals)-1 {
			// Add a subcanvas for padding between
			add(&subCanvasHorizontal{
				actualStartX: 0,
				actualEndX:   c.mergePad,
				img:          image.NewRGBA(image.Rect(0, 0, c.mergePad, c.h)),
			})
		}
	}

	// Add another matrix-width empty subcanvas
	add(&subCanvasHorizontal{
		actualStartX: 0,
		actualEndX:   c.w,
		img:          image.NewRGBA(image.Rect(0, 0, c.w, c.h)),
	})

	c.log.Debug("done initializing sub canvases",
		zap.Int("num", len(c.subCanvases)),
	)

SUBS:
	for _, sub := range c.subCanvases {
		if sub == nil || sub.previous == nil {
			continue SUBS
		}

		prev := sub.previous

		sub.virtualStartX = prev.virtualEndX + 1
		diff := sub.actualEndX - sub.actualStartX
		sub.virtualEndX = sub.virtualStartX + diff

		c.log.Debug("define sub canvas",
			zap.Int("actualstartX", sub.actualStartX),
			zap.Int("min X", sub.img.Bounds().Min.X),
			zap.Int("actualendX", sub.actualEndX),
			zap.Int("max X", sub.img.Bounds().Max.X),
			zap.Int("virtualstartX", sub.virtualStartX),
			zap.Int("virtualendx", sub.virtualEndX),
			zap.Int("actual canvas Width", c.w),
			zap.Int("pad", c.pad),
		)
	}
	c.log.Debug("done defining sub canvases")
}

func (c *ScrollCanvas) rightToLeft(ctx context.Context) error {
	if err := c.horizontalPrep(ctx); err != nil {
		return err
	}

	c.sendScrollSpeedChan = make(chan time.Duration, 1)
	return c.Matrix.Play(ctx, c.GetScrollSpeed(), c.sendScrollSpeedChan)
}

func (c *ScrollCanvas) leftToRight(ctx context.Context) error {
	if err := c.horizontalPrep(ctx); err != nil {
		return err
	}

	// Default is rightToLeft, reverse it
	c.Matrix.ReversePreLoad()

	c.sendScrollSpeedChan = make(chan time.Duration, 1)
	return c.Matrix.Play(ctx, c.GetScrollSpeed(), c.sendScrollSpeedChan)
}

func (c *ScrollCanvas) horizontalPrep(ctx context.Context) error {
	if len(c.subCanvases) < 1 {
		c.PrepareSubCanvases()
	}
	if len(c.subCanvases) < 1 {
		return fmt.Errorf("not enough subcanvases to merge")
	}

	lastSub := c.subCanvases[len(c.subCanvases)-1]
	if lastSub == nil {
		return fmt.Errorf("last subcanvas was nil during horizontalPrep")
	}

	finish := lastSub.virtualEndX

	virtualX := c.subCanvases[0].virtualStartX

	c.log.Debug("performing right to left scroll without canvas merge",
		zap.Int("virtualX start", virtualX),
		zap.Int("finish", finish),
		zap.Duration("interval", c.GetScrollSpeed()),
	)

	sceneIndex := 0
	wg, _ := errgroup.WithContext(ctx)
	if c.preloadThreads > 0 {
		c.log.Info("limiting preload threads for horizontal prep",
			zap.Int("threads", c.preloadThreads),
		)
		wg.SetLimit(c.preloadThreads)
	}
	for {
		if virtualX == finish {
			break
		}

		mySceneIndex, myVirtualX := sceneIndex, virtualX

		wg.Go(func() error {
			buf := getLoader(c.w * c.h)
			defer putLoader(buf)

			c.fillHorizontalFrame(buf, myVirtualX)

			c.Matrix.PreLoad(&matrix.MatrixScene{
				Index:  mySceneIndex,
				Points: buf.points,
			})
			return nil
		})
		sceneIndex++
		virtualX++
	}

	return wg.Wait()
}

// MatchScroll will match the scroll speed of this canvas from the given one.
// It will block until the context is canceled
func (c *ScrollCanvas) MatchScroll(ctx context.Context, match *ScrollCanvas) {
	c.matchScrollCtx, c.matchScrollCancel = context.WithCancel(ctx)
	defer c.matchScrollCancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	for {
		select {
		case <-c.matchScrollCtx.Done():
			return
		case <-ticker.C:
		}
		if curr := match.GetScrollSpeed(); curr != c.GetScrollSpeed() {
			c.SetScrollSpeed(curr)
		}
	}
}

// WithScrollSpeed ...
func WithScrollSpeed(d time.Duration) ScrollCanvasOption {
	return func(c *ScrollCanvas) error {
		c.interval.Store(d)
		return nil
	}
}

// WithScrollDirection ...
func WithScrollDirection(direct ScrollDirection) ScrollCanvasOption {
	return func(c *ScrollCanvas) error {
		c.SetScrollDirection(direct)
		return nil
	}
}

// WithMergePadding ...
func WithMergePadding(pad int) ScrollCanvasOption {
	return func(c *ScrollCanvas) error {
		c.mergePad = pad
		return nil
	}
}

func WithPreloadThreads(t int) ScrollCanvasOption {
	return func(c *ScrollCanvas) error {
		c.preloadThreads = t
		return nil
	}
}

func WithName(name string) ScrollCanvasOption {
	return func(c *ScrollCanvas) error {
		c.name = name
		return nil
	}
}
