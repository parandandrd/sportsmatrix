package clock

import (
	"context"
	"fmt"
	"image/color"
	"net/http"
	"sync"
	"time"

	"github.com/golang/freetype/truetype"
	"github.com/twitchtv/twirp"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
	pb "github.com/parandandrd/sportsmatrix/internal/proto/basicboard"
	"github.com/parandandrd/sportsmatrix/internal/rgbrender"
	"github.com/parandandrd/sportsmatrix/internal/twirphelpers"
	"github.com/parandandrd/sportsmatrix/internal/util"
)

// Name is the default board name for this Clock
var Name = "Clock"

// Clock implements board.Board
type Clock struct {
	config      *Config
	font        *truetype.Font
	textWriters map[int]*rgbrender.TextWriter
	log         *zap.Logger
	rpcServer   pb.TwirpServer
	enabler     board.Enabler
	sync.Mutex
}

// Config is a Clock configuration
type Config struct {
	boardDelay   atomic.Duration
	StartEnabled *atomic.Bool `json:"enabled"`
	BoardDelay   string       `json:"boardDelay"`
	OnTimes      []string     `json:"onTimes"`
	OffTimes     []string     `json:"offTimes"`
	ShowBetween  *atomic.Bool `json:"showBetween"`
	Enable24Hour *atomic.Bool `json:"enable24Hour"`
}

// SetDefaults ...
func (c *Config) SetDefaults() {
	c.boardDelay.Store(board.ParseDelay(c.BoardDelay, 10*time.Second))

	if c.StartEnabled == nil {
		c.StartEnabled = atomic.NewBool(false)
	}

	if c.ShowBetween == nil {
		c.ShowBetween = atomic.NewBool(false)
	}

	if c.Enable24Hour == nil {
		c.Enable24Hour = atomic.NewBool(false)
	}
}

// New returns a new Clock board
func New(config *Config, logger *zap.Logger) (*Clock, error) {
	c := &Clock{
		config:      config,
		log:         logger,
		textWriters: make(map[int]*rgbrender.TextWriter),
		enabler:     enabler.New(),
	}

	if config.StartEnabled.Load() {
		c.enabler.Enable()
	}

	svr := &Server{
		board: c,
	}
	c.rpcServer = pb.NewBasicBoardServer(svr,
		twirp.WithServerPathPrefix("/clock"),
		twirp.ChainHooks(
			twirphelpers.GetDefaultHooks(c, c.log),
		),
	)

	c.log.Debug("registering RPC server for Clock",
		zap.String("prefix", c.rpcServer.PathPrefix()),
	)

	if err := util.SetCrons(config.OnTimes, func() {
		c.log.Info("clock turning on")
		c.Enabler().Enable()
	}); err != nil {
		return nil, err
	}
	if err := util.SetCrons(config.OffTimes, func() {
		c.log.Info("clock turning off")
		c.Enabler().Disable()
	}); err != nil {
		return nil, err
	}

	return c, nil
}

// InBetween ...
func (c *Clock) InBetween() bool {
	return c.config.ShowBetween.Load()
}

// Name ...
func (c *Clock) Name() string {
	return Name
}

func (c *Clock) Enabler() board.Enabler {
	return c.enabler
}

// Cleanup ...
func (c *Clock) Cleanup() {}

// ScrollMode ...
func (c *Clock) ScrollMode() bool {
	return false
}

// Render ...
func (c *Clock) Render(ctx context.Context, canvas board.Canvas) error {
	renderctx, rendercancel := context.WithCancel(ctx)
	defer rendercancel()
	if err := c.render(renderctx, canvas); err != nil {
		return err
	}

	return nil
}

func (c *Clock) currentTimeStr() string {
	ampm := ""
	h, m, _ := time.Now().Local().Clock()
	z := ""
	if m < 10 {
		z = "0"
	}

	if c.config.Enable24Hour.Load() {
		hz := ""
		if h < 10 {
			hz = "0"
		}
		return fmt.Sprintf("%s%d:%s%d", hz, h, z, m)
	}

	if h >= 12 {
		h = h - 12
		ampm = "PM"
	} else {
		ampm = "AM"
	}
	if h == 0 {
		h = 12
	}

	return fmt.Sprintf("%d:%s%d%s", h, z, m, ampm)
}

// Render ...
func (c *Clock) render(ctx context.Context, canvas board.Canvas) error {
	if !c.Enabler().Enabled() {
		return nil
	}

	writer, err := c.getWriter(rgbrender.ZeroedBounds(canvas.Bounds()).Dy())
	if err != nil {
		return err
	}

	update := make(chan struct{})

	clockCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		prevTime := ""
		thisTime := ""
		ticker := time.NewTicker(500 * time.Millisecond)
		for {
			select {
			case <-clockCtx.Done():
				return
			case <-ticker.C:
			}
			thisTime = c.currentTimeStr()
			if thisTime != prevTime {
				select {
				case update <- struct{}{}:
				case <-clockCtx.Done():
					return
				}
			}
			prevTime = thisTime
		}
	}()

	go func() {
		for {
			c.log.Debug("waiting for update")
			select {
			case <-clockCtx.Done():
				return
			case <-update:
			}
			c.log.Debug("done waiting for update")

			if err := writer.WriteAligned(
				rgbrender.CenterCenter,
				canvas,
				canvas.Bounds(),
				[]string{
					c.currentTimeStr(),
				},
				color.White,
			); err != nil {
				c.log.Error("failed to write clock", zap.Error(err))
				return
			}

			c.log.Debug("write non scroll clock",
				zap.String("time", c.currentTimeStr()),
			)

			if err := canvas.Render(ctx); err != nil {
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return context.Canceled
	case <-time.After(c.config.boardDelay.Load()):
	}

	return nil
}

// HasPriority ...
func (c *Clock) HasPriority() bool {
	return false
}

// GetHTTPHandlers ...
func (c *Clock) GetHTTPHandlers() ([]*board.HTTPHandler, error) {
	disable := &board.HTTPHandler{
		Path: "/clock/disable",
		Handler: func(http.ResponseWriter, *http.Request) {
			c.log.Info("disabling clock board")
			c.Enabler().Disable()
		},
	}
	enable := &board.HTTPHandler{
		Path: "/clock/enable",
		Handler: func(http.ResponseWriter, *http.Request) {
			c.log.Info("enabling clock board")
			c.Enabler().Enable()
		},
	}
	status := &board.HTTPHandler{
		Path: "/clock/status",
		Handler: func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			if c.Enabler().Enabled() {
				_, _ = w.Write([]byte("true"))
				return
			}
			_, _ = w.Write([]byte("false"))
		},
	}

	return []*board.HTTPHandler{
		disable,
		enable,
		status,
	}, nil
}

func (c *Clock) getWriter(canvasHeight int) (*rgbrender.TextWriter, error) {
	if w, ok := c.textWriters[canvasHeight]; ok {
		return w, nil
	}

	if c.font == nil {
		var err error
		c.font, err = rgbrender.GetFont("04B_03__.ttf")
		if err != nil {
			return nil, err
		}
	}

	size := 0.5 * float64(canvasHeight)

	c.Lock()
	defer c.Unlock()
	c.textWriters[canvasHeight] = rgbrender.NewTextWriter(c.font, size)
	c.textWriters[canvasHeight].YStartCorrection = -3

	return c.textWriters[canvasHeight], nil
}

// BoardDelay is how long the clock shows for.
func (c *Clock) BoardDelay() time.Duration {
	return c.config.boardDelay.Load()
}

// SetBoardDelay changes how long the clock shows for, from the next time it shows.
func (c *Clock) SetBoardDelay(d time.Duration) {
	c.config.boardDelay.Store(d)
}
