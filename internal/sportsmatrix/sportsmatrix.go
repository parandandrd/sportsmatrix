package sportsmatrix

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/conffile"
	rgb "github.com/parandandrd/sportsmatrix/internal/rgbmatrix-rpi"
)

var version = "noversion"

// SportsMatrix controls the RGB matrix. It rotates through a list of given board.Board
type SportsMatrix struct {
	cfg                *Config
	isServing          chan struct{}
	canvases           []board.Canvas
	boards             []board.Board
	screenIsOn         *atomic.Bool
	serveBlock         chan struct{}
	boardStateChange   chan struct{}
	log                *zap.Logger
	boardCtx           context.Context
	boardCancel        context.CancelFunc
	currentBoardCancel context.CancelFunc
	server             http.Server
	close              chan struct{}
	httpEndpoints      []string
	jumpLock           sync.Mutex
	boardLock          sync.Mutex
	screenSwitch       chan struct{}
	jumpTo             chan string
	betweenBoards      []board.Board
	currentJump        string
	jumping            *atomic.Bool
	switchedOn         int
	switchedOff        int
	switchTestSleep    bool
	serveContext       context.Context
	liveOnly           *atomic.Bool
	cron               *cron.Cron
	screenJobs         []cron.EntryID
	boardSections      map[board.Board]string
	sectionOrder       map[string]int
	configFile         *conffile.File
	// settingsLock is held across changing a setting and saving it
	settingsLock sync.Mutex
	sync.Mutex
}

// Config ...
type Config struct {
	ServeWebUI     bool                `json:"serveWebUI"`
	HTTPListenPort int                 `json:"httpListenPort"`
	HardwareConfig *rgb.HardwareConfig `json:"hardwareConfig"`
	RuntimeOptions *rgb.RuntimeOptions `json:"runtimeOptions"`
	ScreenOffTimes []string            `json:"screenOffTimes"`
	ScreenOnTimes  []string            `json:"screenOnTimes"`
	PreloadThreads int                 `json:"preloadThreads"`
}

// Defaults sets some sane config defaults
func (c *Config) Defaults() {
	if c.RuntimeOptions == nil {
		// copy, don't alias: taking the address of the package-level default
		// hands every Config the same struct, and the writes below then edit
		// the defaults themselves.
		opts := rgb.DefaultRuntimeOptions
		c.RuntimeOptions = &opts
	}
	c.RuntimeOptions.Daemon = 0
	c.RuntimeOptions.DoGPIOInit = true

	if c.HTTPListenPort == 0 {
		c.HTTPListenPort = 8080
	}

	if c.HardwareConfig == nil {
		hw := rgb.DefaultConfig
		c.HardwareConfig = &hw
		c.HardwareConfig.Cols = 64
		c.HardwareConfig.Rows = 32
		// the library's default is full brightness, which is too much
		c.HardwareConfig.Brightness = 60
	}

	if c.HardwareConfig.Rows == 0 {
		c.HardwareConfig.Rows = 32
	}
	if c.HardwareConfig.Cols == 0 {
		c.HardwareConfig.Cols = 64
	}
	// Only a brightness nobody set. This used to turn 100 into 60 too, meant
	// for the library default above, which also overrode a config that asked
	// for 100 on purpose.
	if c.HardwareConfig.Brightness == 0 {
		c.HardwareConfig.Brightness = 60
	}
	if c.HardwareConfig.HardwareMapping == "" {
		c.HardwareConfig.HardwareMapping = "adafruit-hat-pwm"
	}
	if c.HardwareConfig.ChainLength == 0 {
		c.HardwareConfig.ChainLength = 1
	}
	if c.HardwareConfig.Parallel == 0 {
		c.HardwareConfig.Parallel = 1
	}
	if c.HardwareConfig.PWMBits == 0 {
		c.HardwareConfig.PWMBits = 11
	}
	if c.HardwareConfig.PWMLSBNanoseconds == 0 {
		c.HardwareConfig.PWMLSBNanoseconds = 130
	}
}

// New ...
// nolint:contextcheck
func New(ctx context.Context, logger *zap.Logger, cfg *Config, canvases []board.Canvas, boards ...board.Board) (*SportsMatrix, error) {
	cfg.Defaults()

	s := &SportsMatrix{
		boards:           boards,
		cfg:              cfg,
		log:              logger,
		serveBlock:       make(chan struct{}),
		boardStateChange: make(chan struct{}, 1),
		close:            make(chan struct{}),
		screenIsOn:       atomic.NewBool(true),
		isServing:        make(chan struct{}, 1),
		jumpTo:           make(chan string, 1),
		canvases:         canvases,
		jumping:          atomic.NewBool(false),
		screenSwitch:     make(chan struct{}, 1),
		liveOnly:         atomic.NewBool(false),
	}

	s.boardCtx, s.boardCancel = context.WithCancel(context.Background())

	for _, b := range s.boards {
		s.log.Info("Registering board", zap.String("board", b.Name()))
		// So the serve loop can sleep while every board is disabled and still
		// wake the moment one is turned back on.
		b.Enabler().SetStateChangeCallback(s.notifyBoardStateChange)
	}

	s.cron = cron.New()
	if err := s.scheduleScreen(s.cfg.ScreenOnTimes, s.cfg.ScreenOffTimes); err != nil {
		return nil, err
	}
	s.cron.Start()

	return s, nil
}

// scheduleScreen replaces the jobs that turn the screen on and off. Every
// expression is checked before any job is touched, so a bad one leaves the
// schedule as it was. Callers other than New hold settingsLock.
func (s *SportsMatrix) scheduleScreen(on, off []string) error {
	for _, spec := range append(append([]string(nil), on...), off...) {
		if _, err := cron.ParseStandard(spec); err != nil {
			return fmt.Errorf("%w: %q: %w", errBadSchedule, spec, err)
		}
	}

	for _, id := range s.screenJobs {
		s.cron.Remove(id)
	}
	s.screenJobs = nil

	for _, spec := range off {
		s.log.Info("Screen will be scheduled to turn off", zap.String("turn off", spec))
		id, err := s.cron.AddFunc(spec, func() {
			s.log.Warn("Turning screen off!")
			if err := s.ScreenOff(context.Background()); err != nil {
				s.log.Error("failed to turn screen off during ScreenOfftimes",
					zap.Error(err),
				)
			}
		})
		if err != nil {
			return fmt.Errorf("failed to add cron for screen off times: %w", err)
		}
		s.screenJobs = append(s.screenJobs, id)
	}
	for _, spec := range on {
		s.log.Info("Screen will be scheduled to turn on", zap.String("turn on", spec))
		id, err := s.cron.AddFunc(spec, func() {
			s.log.Warn("Turning screen on!")
			if err := s.ScreenOn(context.Background()); err != nil {
				s.log.Error("failed to turn screen on during ScreenOnTimes",
					zap.Error(err),
				)
			}
		})
		if err != nil {
			return fmt.Errorf("failed to add cron for screen on times: %w", err)
		}
		s.screenJobs = append(s.screenJobs, id)
	}

	s.cfg.ScreenOnTimes = on
	s.cfg.ScreenOffTimes = off

	return nil
}

// SetBoardSections records which top-level config file section each board was
// built from, and the order the file lists its sections in, so ListBoards can
// lay boards out the way the config file does. Keys are matched without regard
// to case, as the config file itself is read.
//
// The boards take the config file's order too: the panel cycles through them
// in the order their sections come in the file.
func (s *SportsMatrix) SetBoardSections(sections map[board.Board]string, fileOrder []string) {
	order := sectionPositions(fileOrder)

	s.Lock()
	defer s.Unlock()
	s.boardSections = sections
	s.sectionOrder = order
	s.sortBoards()
}

// sortBoards puts the boards in the order of the config file's sections,
// keeping each section's own boards in the order they were built, and boards
// whose section isn't in the file last. It replaces the slice rather than
// sorting it in place, since the serve loop may be ranging over the old one.
// Callers hold the lock.
func (s *SportsMatrix) sortBoards() {
	position := func(b board.Board) int {
		section := s.boardSections[b]
		if i, ok := s.sectionOrder[strings.ToLower(section)]; ok && section != "" {
			return i
		}
		return len(s.sectionOrder)
	}

	sorted := append([]board.Board(nil), s.boards...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return position(sorted[i]) < position(sorted[j])
	})
	s.boards = sorted
}

// boardList is the boards the serve loop cycles through, as they are now. The
// slice is only ever replaced, never changed, so a caller can range over it.
func (s *SportsMatrix) boardList() []board.Board {
	s.Lock()
	defer s.Unlock()
	return s.boards
}

func sectionPositions(fileOrder []string) map[string]int {
	order := make(map[string]int, len(fileOrder))
	for i, key := range fileOrder {
		k := strings.ToLower(key)
		if _, dup := order[k]; !dup {
			order[k] = i
		}
	}
	return order
}

// AddBetweenBoard adds a board to be run between each enabled board
func (s *SportsMatrix) AddBetweenBoard(board board.Board) {
	s.betweenBoards = append(s.betweenBoards, board)
}

// ScreenOn turns the matrix on
func (s *SportsMatrix) ScreenOn(ctx context.Context) error {
	// The screenSwitch channel is used just like a sync.Mutex, but with
	// a timeout. It has a buffer size of 1. This is so we don't try to turn the screen
	// off or on at the same time and that we don't pool up a bunch of on/off requests

	select {
	case s.screenSwitch <- struct{}{}:
	case <-time.After(2 * time.Second):
		return fmt.Errorf("timed out waiting for switch lock")
	case <-ctx.Done():
		return context.Canceled
	}

	defer func() {
		<-s.screenSwitch
	}()

	if changed := s.screenIsOn.CompareAndSwap(false, true); !changed {
		s.log.Warn("screen is already on")
		return nil
	}

	if s.switchTestSleep {
		s.switchedOn++
	}
	s.log.Warn("screen turning on")
	select {
	case s.serveBlock <- struct{}{}:
	case <-time.After(10 * time.Second):
		s.log.Error("timed out while trying to unblock serveBlock")
	}

	return nil
}

// ScreenOff turns the matrix off
func (s *SportsMatrix) ScreenOff(ctx context.Context) error {
	select {
	case s.screenSwitch <- struct{}{}:
	case <-time.After(2 * time.Second):
		return fmt.Errorf("timed out waiting for switch lock")
	case <-ctx.Done():
		return context.Canceled
	}

	defer func() {
		<-s.screenSwitch
	}()

	if changed := s.screenIsOn.CompareAndSwap(true, false); !changed {
		s.log.Warn("screen is already off")
		return nil
	}

	s.log.Warn("screen turning off")

	if s.switchTestSleep {
		s.switchedOff++
	}

	s.boardCancel()
	for _, canvas := range s.canvases {
		_ = canvas.Clear()
	}

	s.boardCtx, s.boardCancel = context.WithCancel(s.serveContext)

	return nil
}

// startServices starts the HTTP/RPC services for the boards and the matrix itself
func (s *SportsMatrix) startServices(ctx context.Context) error {
	errChan := s.startHTTP()

	// check for startup error
	s.log.Debug("checking http server for startup error")
	select {
	case <-ctx.Done():
		return context.Canceled
	case err := <-errChan:
		if err != nil {
			return err
		}
	default:
	}

	go func() {
		for {
			select {
			case err := <-errChan:
				s.log.Error("http server failed", zap.Error(err))
			case <-s.close:
				return
			}
		}
	}()

	return nil
}

// allDisabledWait is how long the serve loop sleeps between checks when every
// board is disabled and no Enabler has reported a state change.
const allDisabledWait = 5 * time.Second

// notifyBoardStateChange wakes the serve loop when a board is enabled or
// disabled. It runs on whatever goroutine flipped the board -- an RPC handler,
// usually -- so the send must never block.
func (s *SportsMatrix) notifyBoardStateChange() {
	select {
	case s.boardStateChange <- struct{}{}:
	default:
	}
}

// Serve blocks until the context is canceled
func (s *SportsMatrix) Serve(ctx context.Context) error {
	// Set before the HTTP server starts, since its handlers read it.
	s.serveContext = ctx

	if err := s.startServices(ctx); err != nil {
		return err
	}

	defer func() {
		for _, canvas := range s.canvases {
			_ = canvas.Close()
		}
	}()

	s.boardCtx, s.boardCancel = context.WithCancel(ctx)
	defer s.boardCancel()

	if len(s.boardList()) < 1 {
		return fmt.Errorf("no boards configured")
	}

	clearer := sync.Once{}

	// This is really only for testing.
	setServing := func() {
		select {
		case s.isServing <- struct{}{}:
		default:
		}
	}

	setServingOnce := sync.Once{}

	boardOrder := []string{}
	for _, b := range s.boardList() {
		boardOrder = append(boardOrder, b.Name())

		for _, inb := range s.betweenBoards {
			boardOrder = append(boardOrder, inb.Name())
		}
	}

	s.log.Info("Board Render order",
		zap.Strings("order", boardOrder),
	)

	for {
		select {
		case <-ctx.Done():
			s.log.Warn("context canceled during matrix loop")
			return context.Canceled
		default:
		}

		if s.allDisabled() {
			clearer.Do(func() {
				for _, canvas := range s.canvases {
					if err := canvas.Clear(); err != nil {
						s.log.Error("failed to clear matrix when all boards were disabled", zap.Error(err))
					}
				}
			})

			// Wait for a board to come back rather than spinning. Enabling one
			// signals boardStateChange, so this wakes immediately in the normal
			// case; the timer is the fallback for any Enabler implementation
			// that does not report its own state changes. Without this the loop
			// re-checks allDisabled() as fast as the CPU allows -- tens of
			// millions of times a second, one core pegged, until a board comes
			// back on.
			select {
			case <-ctx.Done():
				s.log.Warn("context canceled while all boards were disabled")
				return context.Canceled
			case <-s.boardStateChange:
			case <-time.After(allDisabledWait):
			}

			continue
		}

		clearer = sync.Once{}

		if !s.screenIsOn.Load() {
			s.log.Warn("screen is turned off")

			// Block until the screen is turned back on
			select {
			case <-ctx.Done():
				s.log.Warn("context canceled while waiting for screen to come back on")
				return context.Canceled
			case <-s.serveBlock:
				s.log.Warn("screen is back on")
				continue
			}
		}

		setServingOnce.Do(setServing)

		s.serveLoop(s.boardCtx)
	}
}

// nextBoard skips the board showing now, if there is one. currentBoardCancel
// is only set and called under the lock.
func (s *SportsMatrix) nextBoard() {
	s.Lock()
	defer s.Unlock()

	if s.currentBoardCancel != nil {
		s.currentBoardCancel()
	}
}

func (s *SportsMatrix) serveLoop(ctx context.Context) {
BOARDS:
	for _, b := range s.boardList() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		boardCtx, boardCancel := context.WithCancel(ctx)
		s.Lock()
		s.currentBoardCancel = boardCancel
		s.Unlock()

		if err := s.doBoard(boardCtx, b); err != nil {
			boardCancel()
			continue BOARDS
		}

		if b.Enabler().Enabled() {
		BETWEEN_BOARDS:
			for _, between := range s.betweenBoards {
				select {
				case <-ctx.Done():
					return
				case <-boardCtx.Done():
					s.log.Debug("current board context canceled while rendering in-between boards",
						zap.String("board", b.Name()),
						zap.String("in-between", between.Name()),
					)
					continue BOARDS
				default:
				}
				s.log.Debug("rendering in-between board",
					zap.String("board", between.Name()),
					zap.String("prior board", b.Name()),
				)
				if err := s.doBoard(boardCtx, between); err != nil {
					continue BETWEEN_BOARDS
				}
			}
		}

		boardCancel()
	}
}

func (s *SportsMatrix) doBoard(ctx context.Context, b board.Board) error {
	select {
	case <-ctx.Done():
		return context.Canceled
	default:
	}

	s.boardLock.Lock()
	defer s.boardLock.Unlock()

	select {
	case <-ctx.Done():
		s.log.Error("serve loop context was canceled",
			zap.String("board", b.Name()),
		)
		return context.Canceled
	case j := <-s.jumpTo:
		s.currentJump = j
	default:
	}

	if s.currentJump != "" {
		if !strings.EqualFold(b.Name(), s.currentJump) {
			return nil
		}
		s.log.Info("jumping to board",
			zap.String("board", b.Name()),
		)
	}

	s.currentJump = ""

	s.log.Debug("Processing board", zap.String("board", b.Name()))

	if !b.Enabler().Enabled() {
		// s.log.Debug("skipping disabled board", zap.String("board", b.Name()))
		return nil
	}

	var wg sync.WaitGroup

	// Each canvas renders on its own goroutine, so the error they report back
	// has to be recorded under a lock rather than assigned to a shared var.
	var (
		errLock  sync.Mutex
		boardErr error
	)

CANVASES:
	for _, canvas := range s.canvases {
		if !canvas.Enabled() {
			// s.log.Warn("canvas is disabled, skipping", zap.String("canvas", canvas.Name()))
			continue CANVASES
		}

		wg.Add(1)
		go func(canvas board.Canvas) {
			defer wg.Done()
			s.log.Debug("rendering board", zap.String("board", b.Name()))
			if err := b.Render(ctx, canvas); err != nil {
				errLock.Lock()
				if boardErr == nil {
					boardErr = err
				}
				errLock.Unlock()
				s.log.Error("board render returned error",
					zap.Error(err),
				)
			}
		}(canvas)
	}
	done := make(chan struct{})

	go func() {
		defer close(done)
		wg.Wait()
	}()

	s.log.Debug("waiting for canvases to be rendered to")
	select {
	case <-ctx.Done():
		s.log.Error("context canceled waiting for canvases to render")
		return context.Canceled
	case <-done:
	}
	s.log.Debug("done waiting for canvases")

	errLock.Lock()
	defer errLock.Unlock()

	return boardErr
}

// Close closes the matrix
func (s *SportsMatrix) Close() {
	s.close <- struct{}{}
	s.server.Close()
}

func (s *SportsMatrix) allDisabled() bool {
	for _, b := range s.boardList() {
		if b.Enabler().Enabled() {
			return false
		}
	}

	return true
}

// JumpTo jumps to a board with a given name
func (s *SportsMatrix) JumpTo(ctx context.Context, boardName string) error {
	s.jumpLock.Lock()
	defer s.jumpLock.Unlock()

	s.jumping.Store(true)
	defer s.jumping.Store(false)

	// a copy: appending to the live slice can write into its spare capacity
	boards := append(append([]board.Board(nil), s.boardList()...), s.betweenBoards...)

	for _, b := range boards {
		if strings.EqualFold(b.Name(), boardName) {
			b.Enabler().Enable()

			defer func() {
				if err := s.ScreenOn(context.Background()); err != nil {
					s.log.Error("failed to turn screen back on after jump",
						zap.String("board", b.Name()),
						zap.Error(err),
					)
				}
			}()

			if err := s.ScreenOff(ctx); err != nil {
				s.log.Error("error while jumping board trying to turn screen off",
					zap.String("board", b.Name()),
					zap.Error(err),
				)
			}

			select {
			case s.jumpTo <- b.Name():
			case <-ctx.Done():
				s.log.Error("context canceled while setting jump board",
					zap.String("board", b.Name()),
				)
				return context.Canceled
			}

			if err := s.ScreenOn(context.Background()); err != nil {
				s.log.Error("failed to turn screen back on after jump",
					zap.String("board", b.Name()),
					zap.Error(err),
				)
			}

			return nil
		}
	}

	return fmt.Errorf("could not find board %s to jump to", boardName)
}
