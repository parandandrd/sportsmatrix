package matrix

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ConsoleMatrix prints a representation of a matrix to a terminal.
// Useful for testing layouts without a Pi or an LED matrix.
type ConsoleMatrix struct {
	matrix      []uint32
	width       int
	height      int
	out         io.Writer
	preload     [][]uint32
	log         *zap.Logger
	preloadLock sync.Mutex
}

// NewConsoleMatrix ...
func NewConsoleMatrix(width int, height int, out io.Writer, logger *zap.Logger) *ConsoleMatrix {
	c := &ConsoleMatrix{
		width:  width,
		height: height,
		matrix: make([]uint32, (width * height)),
		out:    out,
		log:    logger,
	}

	c.Reset()

	return c
}

// Reset ...
func (c *ConsoleMatrix) Reset() {
	for i := range c.matrix {
		c.matrix[i] = colorToUint32(color.Black)
	}
}

// Geometry ...
func (c *ConsoleMatrix) Geometry() (int, int) {
	return c.width, c.height
}

func (c *ConsoleMatrix) position(x int, y int) int {
	return x + (y * c.width)
}

// At ...
func (c *ConsoleMatrix) At(x int, y int) color.Color {
	position := c.position(x, y)
	if position > len(c.matrix)-1 || position < 0 {
		return color.Black
	}

	return uint32ToColorGo(c.matrix[position])
}

// Set ...
func (c *ConsoleMatrix) Set(x int, y int, clr color.Color) {
	position := c.position(x, y)
	if position > len(c.matrix)-1 || position < 0 {
		return
	}

	c.matrix[position] = colorToUint32(clr)
}

func (c *ConsoleMatrix) PreLoad(scene *MatrixScene) {
	c.preloadLock.Lock()
	defer c.preloadLock.Unlock()

	w, h := c.Geometry()
	c.growPreload(scene.Index + 1)

	// Reuse the buffer already at this index rather than allocating a frame's
	// worth every time. A fresh buffer per frame was the largest single source
	// of garbage in a scroll, and zeroing them showed on the profile besides.
	prep := c.preload[scene.Index]
	if len(prep) != w*h {
		prep = make([]uint32, w*h)
		c.preload[scene.Index] = prep
	} else {
		clear(prep)
	}

	for _, pt := range scene.Points {
		position := c.position(pt.X, pt.Y)
		prep[position] = rgbaToUint32(pt.Color)
	}
}

// growPreload extends the frame list to n, keeping the buffers already
// allocated and doubling capacity, so a long scroll does not recopy the outer
// slice on every frame.
func (c *ConsoleMatrix) growPreload(n int) {
	if len(c.preload) >= n {
		return
	}

	if cap(c.preload) >= n {
		c.preload = c.preload[:n]

		return
	}

	grown := make([][]uint32, n, 2*n)
	copy(grown, c.preload)
	c.preload = grown
}

func (c *ConsoleMatrix) ReversePreLoad() {
	for i, j := 0, len(c.preload)-1; i < j; i, j = i+1, j-1 {
		c.preload[i], c.preload[j] = c.preload[j], c.preload[i]
	}
}

func (c *ConsoleMatrix) Play(ctx context.Context, startInterval time.Duration, interval <-chan time.Duration) error {
	defer func() {
		// Truncate rather than discard: the frame buffers stay in the backing
		// array and the next scroll reuses them.
		c.preload = c.preload[:0]
	}()
	waitInterval := startInterval
	c.log.Info("Play matrix",
		zap.Duration("default interval", waitInterval),
	)
	for _, leds := range c.preload {
		// An updated interval can be sent to the channel to change scroll speed
		select {
		case <-ctx.Done():
			return context.Canceled
		case waitInterval = <-interval:
			c.log.Info("RGB matrix got new interval during play",
				zap.Duration("interval", waitInterval),
			)
		default:
		}

		select {
		case <-ctx.Done():
			return context.Canceled
		case <-time.After(waitInterval):
		}

		if err := c.render(leds); err != nil {
			return err
		}
	}

	return nil
}

// Render ...
func (c *ConsoleMatrix) Render() error {
	return c.render(c.matrix)
}

func (c *ConsoleMatrix) render(leds []uint32) error {
	rendered := []string{
		strings.Repeat("_ ", c.width+1),
	}
	row := ""
	for index, clrint := range leds {
		clr := uint32ToColorGo(clrint)
		if (index)%c.width == 0 {
			// This is a new row
			row += "|"
			rendered = append(rendered, row)
			row = "|"
		}
		if clr == nil {
			row += "  "
			continue
		}

		r, g, b, _ := clr.RGBA()

		if r > g && r > b {
			row += "R "
		} else if g > r && g > b {
			row += "G "
		} else if b > r && b > g {
			row += "B "
		} else if r < 40 && g < 40 && b < 40 {
			row += "  "
		} else if r > 240 && g > 240 && b > 240 {
			row += "W "
		} else {
			row += "0 "
		}
	}
	rendered = append(rendered, row)

	rendered = append(rendered, strings.Repeat("_ ", c.width+1)+"|")

	_, _ = fmt.Fprintln(c.out, strings.Join(rendered, "\n"))

	c.Reset()

	return nil
}

// Close ...
func (c *ConsoleMatrix) Close() error {
	return nil
}

// SetBrightness does nothing
func (c *ConsoleMatrix) SetBrightness(brightness int) {
}
