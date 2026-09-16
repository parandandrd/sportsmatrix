package calendarboard

import (
	"context"
	"fmt"
	"image"
	"net/http"
	"strings"
	"time"

	"github.com/twitchtv/twirp"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
	"github.com/parandandrd/sportsmatrix/internal/logo"
	"github.com/parandandrd/sportsmatrix/internal/rgbrender"
	"github.com/parandandrd/sportsmatrix/internal/twirphelpers"
	"github.com/parandandrd/sportsmatrix/internal/util"

	pb "github.com/parandandrd/sportsmatrix/internal/proto/basicboard"
)

// CalendarBoard implements board.Board
type CalendarBoard struct {
	config         *Config
	api            API
	log            *zap.Logger
	scheduleWriter *rgbrender.TextWriter
	rpcServer      pb.TwirpServer
	boardCtx       context.Context
	boardCancel    context.CancelFunc
	logo           *logo.Logo
	enabler        board.Enabler
}

// Todayer is a func that returns a string representing a date
// that will be used for determining "Today's" games.
// This is useful in testing what past days looked like
type Todayer func() []time.Time

// Config ...
type Config struct {
	TodayFunc          Todayer
	boardDelay         atomic.Duration
	StartEnabled       *atomic.Bool `json:"enabled"`
	BoardDelay         string       `json:"boardDelay"`
	OnTimes            []string     `json:"onTimes"`
	OffTimes           []string     `json:"offTimes"`
	TightScrollPadding int          `json:"tightScrollPadding"`
	CalendarIDs        []string     `json:"calendarIDs"`
}

// API ...
type API interface {
	CalendarIcon(ctx context.Context, bounds image.Rectangle) (*logo.Logo, error)
	HTTPPathPrefix() string
	DailyEvents(ctx context.Context, date time.Time) ([]*Event, error)
}

// Event is a calendar event
type Event struct {
	Time  time.Time
	Title string
}

// SetDefaults sets config defaults
func (c *Config) SetDefaults() {
	c.boardDelay.Store(board.ParseDelay(c.BoardDelay, 10*time.Second))

	if c.StartEnabled == nil {
		c.StartEnabled = atomic.NewBool(false)
	}
}

// New ...
func New(api API, logger *zap.Logger, config *Config) (*CalendarBoard, error) {
	s := &CalendarBoard{
		config:  config,
		api:     api,
		log:     logger,
		enabler: enabler.New(),
	}

	if config.StartEnabled.Load() {
		s.enabler.Enable()
	}

	s.log.Info("Register Calendar Board",
		zap.String("board name", s.Name()),
	)

	if s.config.TodayFunc == nil {
		s.config.TodayFunc = util.TodayFunc()
	}

	if err := util.SetCrons(config.OnTimes, func() {
		s.log.Info("calendarboard turning on")
		s.Enabler().Enable()
	}); err != nil {
		return nil, err
	}
	if err := util.SetCrons(config.OffTimes, func() {
		s.log.Info("calendarboard turning off")
		s.Enabler().Disable()
	}); err != nil {
		return nil, err
	}

	svr := &Server{
		board: s,
	}
	prfx := s.api.HTTPPathPrefix()
	if !strings.HasPrefix(prfx, "/") {
		prfx = fmt.Sprintf("/%s", prfx)
	}
	s.rpcServer = pb.NewBasicBoardServer(svr,
		twirp.WithServerPathPrefix(prfx),
		twirp.ChainHooks(
			twirphelpers.GetDefaultHooks(s, s.log),
		),
	)

	return s, nil
}

// Name ...
func (s *CalendarBoard) Name() string {
	return s.api.HTTPPathPrefix()
}

func (s *CalendarBoard) Enabler() board.Enabler {
	return s.enabler
}

// InBetween ...
func (s *CalendarBoard) InBetween() bool {
	return false
}

// ScrollMode ...
func (s *CalendarBoard) ScrollMode() bool {
	return false
}

// HasPriority ...
func (s *CalendarBoard) HasPriority() bool {
	return false
}

// GetHTTPHandlers ...
func (s *CalendarBoard) GetHTTPHandlers() ([]*board.HTTPHandler, error) {
	return nil, nil
}

// GetRPCHandler ...
func (s *CalendarBoard) GetRPCHandler() (string, http.Handler) {
	return s.rpcServer.PathPrefix(), s.rpcServer
}

// BoardDelay is how long each event shows for.
func (s *CalendarBoard) BoardDelay() time.Duration {
	return s.config.boardDelay.Load()
}

// SetBoardDelay changes how long each event shows for, from the next time it shows.
func (s *CalendarBoard) SetBoardDelay(d time.Duration) {
	s.config.boardDelay.Store(d)
}
