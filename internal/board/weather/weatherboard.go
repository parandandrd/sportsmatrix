// Package weatherboard draws the weather: current conditions and the next few
// hours on one board, and the coming days on another. Both come from one
// weather.Source, which fetches for them.
package weatherboard

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/twitchtv/twirp"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
	pb "github.com/parandandrd/sportsmatrix/internal/proto/basicboard"
	"github.com/parandandrd/sportsmatrix/internal/twirphelpers"
	"github.com/parandandrd/sportsmatrix/internal/util"
	"github.com/parandandrd/sportsmatrix/internal/weather"
)

// Names of the boards, which is what Jump takes.
const (
	CurrentName  = "Current Conditions"
	ForecastName = "Forecast"
)

// Config is the weatherConfig section. The section's own settings are the
// current conditions board's, and the location, provider and units that both
// boards share. The forecast board's are in its forecast block.
type Config struct {
	boardDelay   atomic.Duration
	StartEnabled *atomic.Bool `json:"enabled"`
	BoardDelay   string       `json:"boardDelay"`
	OnTimes      []string     `json:"onTimes"`
	OffTimes     []string     `json:"offTimes"`

	// Provider is "nws", the US National Weather Service, or "open-meteo".
	Provider string `json:"provider"`
	// Location is a latitude and longitude, "41.8858, -87.6181".
	Location string `json:"location"`
	// Units is "imperial" or "metric".
	Units string `json:"units"`

	Forecast *ForecastConfig `json:"forecast"`
}

// ForecastConfig is the forecast board's block of the weatherConfig section.
type ForecastConfig struct {
	boardDelay   atomic.Duration
	StartEnabled *atomic.Bool `json:"enabled"`
	BoardDelay   string       `json:"boardDelay"`
	OnTimes      []string     `json:"onTimes"`
	OffTimes     []string     `json:"offTimes"`
	// Days is how many days the forecast shows, starting tomorrow.
	Days int `json:"days"`
}

const (
	defaultDelay = 10 * time.Second
	defaultDays  = 3
	maxDays      = 6
)

// SetDefaults fills in what the config file leaves out.
func (c *Config) SetDefaults() {
	c.boardDelay.Store(board.ParseDelay(c.BoardDelay, defaultDelay))
	if c.StartEnabled == nil {
		c.StartEnabled = atomic.NewBool(false)
	}

	if c.Forecast == nil {
		c.Forecast = &ForecastConfig{}
	}
	f := c.Forecast
	f.boardDelay.Store(board.ParseDelay(f.BoardDelay, defaultDelay))
	if f.StartEnabled == nil {
		f.StartEnabled = atomic.NewBool(false)
	}
	if f.Days < 1 {
		f.Days = defaultDays
	}
	if f.Days > maxDays {
		f.Days = maxDays
	}
}

// New builds the current conditions board and the forecast board. A bad
// provider, units or location setting is logged and left at its default rather
// than stopping the matrix from starting: the boards say nothing until the
// location is set right, which can be done from the web UI.
func New(config *Config, logger *zap.Logger) (*CurrentBoard, *ForecastBoard, error) {
	provider, err := weather.NewProvider(config.Provider, weather.NewClient())
	if err != nil {
		logger.Error("weather provider not known, using the Weather Service", zap.Error(err))
		provider, _ = weather.NewProvider("", weather.NewClient())
	}
	return newBoards(config, provider, logger)
}

func newBoards(config *Config, provider weather.Provider, logger *zap.Logger) (*CurrentBoard, *ForecastBoard, error) {
	units, err := weather.ParseUnits(config.Units)
	if err != nil {
		logger.Error("weather units not known, using imperial", zap.Error(err))
	}

	var loc *weather.Location
	if config.Location != "" {
		l, err := weather.ParseLocation(config.Location)
		if err != nil {
			logger.Error("weather location can't be read; set it in the web UI or the config file", zap.Error(err))
		} else {
			loc = &l
		}
	}

	src := weather.NewSource(provider, units, loc, logger)

	current := &CurrentBoard{
		common: newCommon(CurrentName, src, logger),
		config: config,
	}
	forecast := &ForecastBoard{
		common: newCommon(ForecastName, src, logger),
		config: config.Forecast,
	}

	if config.StartEnabled.Load() {
		current.enabler.Enable()
	}
	if config.Forecast.StartEnabled.Load() {
		forecast.enabler.Enable()
	}

	if err := current.start(current, "/weather", config.OnTimes, config.OffTimes); err != nil {
		return nil, nil, err
	}
	if err := forecast.start(forecast, "/forecast/weather", config.Forecast.OnTimes, config.Forecast.OffTimes); err != nil {
		return nil, nil, err
	}

	return current, forecast, nil
}

// common is what both weather boards have.
type common struct {
	name      string
	src       *weather.Source
	log       *zap.Logger
	enabler   board.Enabler
	rpcServer pb.TwirpServer
	draw      *drawer

	// lastProblem keeps a board that can't get the weather from logging it
	// every time it comes round.
	lock        sync.Mutex
	lastProblem string
}

func newCommon(name string, src *weather.Source, logger *zap.Logger) common {
	return common{
		name:    name,
		src:     src,
		log:     logger,
		enabler: enabler.New(),
		draw:    &drawer{},
	}
}

func (c *common) start(b board.Board, prefix string, on, off []string) error {
	c.rpcServer = pb.NewBasicBoardServer(&server{board: b},
		twirp.WithServerPathPrefix(prefix),
		twirp.ChainHooks(twirphelpers.GetDefaultHooks(b, c.log)),
	)

	if err := util.SetCrons(on, func() {
		c.log.Info("weather board turning on", zap.String("board", c.name))
		c.enabler.Enable()
	}); err != nil {
		return err
	}
	return util.SetCrons(off, func() {
		c.log.Info("weather board turning off", zap.String("board", c.name))
		c.enabler.Disable()
	})
}

// Name ...
func (c *common) Name() string { return c.name }

// Enabler ...
func (c *common) Enabler() board.Enabler { return c.enabler }

// InBetween ...
func (c *common) InBetween() bool { return false }

// GetHTTPHandlers ...
func (c *common) GetHTTPHandlers() ([]*board.HTTPHandler, error) { return nil, nil }

// GetRPCHandler ...
func (c *common) GetRPCHandler() (string, http.Handler) {
	return c.rpcServer.PathPrefix(), c.rpcServer
}

// report gets the weather, or nil when there is none to show, in which case
// the board skips its turn. Why is logged once, until it changes.
func (c *common) report(ctx context.Context) *weather.Report {
	r, err := c.src.Report(ctx)

	problem := ""
	if err != nil {
		problem = err.Error()
	}

	c.lock.Lock()
	changed := problem != c.lastProblem
	c.lastProblem = problem
	c.lock.Unlock()

	if err != nil {
		if changed {
			c.log.Warn("weather board has nothing to show", zap.String("board", c.name), zap.Error(err))
		}
		return nil
	}

	return r
}

// CurrentBoard shows the weather now, then the next few hours.
type CurrentBoard struct {
	common
	config *Config
}

// BoardDelay is how long each screen shows for.
func (b *CurrentBoard) BoardDelay() time.Duration { return b.config.boardDelay.Load() }

// SetBoardDelay changes how long each screen shows for.
func (b *CurrentBoard) SetBoardDelay(d time.Duration) { b.config.boardDelay.Store(d) }

// Location is where both weather boards report for, as it would be written in
// the config file, and the name of the place when the provider gives one.
// Empty when no location is set.
func (b *CurrentBoard) Location(ctx context.Context) (string, string) {
	loc, ok := b.src.Location()
	if !ok {
		return "", ""
	}

	place := ""
	if r, err := b.src.Report(ctx); err == nil {
		place = r.Place
	}
	return loc.String(), place
}

// SetLocation moves both weather boards to the coordinates in s, once the
// provider has said it has weather there. It returns the location as it should
// be saved.
func (b *CurrentBoard) SetLocation(ctx context.Context, s string) (string, error) {
	loc, err := weather.ParseLocation(s)
	if err != nil {
		return "", err
	}
	if err := b.src.SetLocation(ctx, loc); err != nil {
		return "", weather.SettingError(err)
	}
	return loc.String(), nil
}

// ForecastBoard shows the coming days.
type ForecastBoard struct {
	common
	config *ForecastConfig
}

// BoardDelay is how long each screen shows for.
func (b *ForecastBoard) BoardDelay() time.Duration { return b.config.boardDelay.Load() }

// SetBoardDelay changes how long each screen shows for.
func (b *ForecastBoard) SetBoardDelay(d time.Duration) { b.config.boardDelay.Store(d) }
