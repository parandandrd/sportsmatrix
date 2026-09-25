// Package tvboard shows when followed TV shows have new episodes coming up,
// from TVmaze. It skips its turn when none of them has one in the next week.
package tvboard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/twitchtv/twirp"
	"go.uber.org/atomic"
	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
	pb "github.com/parandandrd/sportsmatrix/internal/proto/basicboard"
	"github.com/parandandrd/sportsmatrix/internal/tvmaze"
	"github.com/parandandrd/sportsmatrix/internal/twirphelpers"
	"github.com/parandandrd/sportsmatrix/internal/util"
)

// Name is the board's name, which is what Jump takes.
const Name = "TV Shows"

// Config is the tvConfig section.
type Config struct {
	boardDelay   atomic.Duration
	StartEnabled *atomic.Bool `json:"enabled"`
	BoardDelay   string       `json:"boardDelay"`
	OnTimes      []string     `json:"onTimes"`
	OffTimes     []string     `json:"offTimes"`
	// LookaheadDays is how far ahead to look for new episodes.
	LookaheadDays int `json:"lookaheadDays"`
	// Shows are the shows to follow, as "Family Guy (tvmaze 84)". A name
	// without an ID is looked up on TVmaze.
	Shows []string `json:"shows"`
}

const (
	defaultDelay     = 10 * time.Second
	defaultLookahead = 7
	maxLookahead     = 60
)

// SetDefaults fills in what the config file leaves out.
func (c *Config) SetDefaults() {
	c.boardDelay.Store(board.ParseDelay(c.BoardDelay, defaultDelay))
	if c.StartEnabled == nil {
		c.StartEnabled = atomic.NewBool(false)
	}
	if c.LookaheadDays < 1 {
		c.LookaheadDays = defaultLookahead
	}
	if c.LookaheadDays > maxLookahead {
		c.LookaheadDays = maxLookahead
	}
}

// API is what the board needs from TVmaze.
type API interface {
	Upcoming(ctx context.Context, id int, from, to time.Time) (tvmaze.Show, []tvmaze.Episode, error)
	Show(ctx context.Context, id int) (tvmaze.Show, *tvmaze.Episode, error)
	Find(ctx context.Context, name string) (tvmaze.Show, error)
	Search(ctx context.Context, query string) ([]tvmaze.Show, error)
}

// refreshEvery is how often the board asks TVmaze again. Schedules don't
// change by the minute.
const refreshEvery = 3 * time.Hour

// staleLimit is how long the board keeps showing what it last found when
// asking again fails.
const staleLimit = 24 * time.Hour

// failRetry is how long the board waits after a failed refresh before trying
// again.
const failRetry = 10 * time.Minute

// Board implements board.Board.
type Board struct {
	config    *Config
	api       API
	log       *zap.Logger
	enabler   board.Enabler
	rpcServer pb.TwirpServer
	draw      *drawer
	now       func() time.Time

	lock     sync.Mutex
	entries  []tvmaze.Entry
	shows    map[int]tvmaze.Show
	found    map[string]int
	episodes []tvmaze.Episode
	fetched  time.Time
	lastFail time.Time
	problem  string
}

// New returns the TV board.
func New(config *Config, logger *zap.Logger) (*Board, error) {
	return newBoard(config, tvmaze.New(tvmaze.NewHTTPClient(), ""), logger)
}

func newBoard(config *Config, api API, logger *zap.Logger) (*Board, error) {
	b := &Board{
		config:  config,
		api:     api,
		log:     logger,
		enabler: enabler.New(),
		draw:    &drawer{},
		now:     time.Now,
		shows:   make(map[int]tvmaze.Show),
		found:   make(map[string]int),
	}
	for _, s := range config.Shows {
		if e := tvmaze.ParseEntry(s); e.Name != "" || e.ID != 0 {
			b.entries = append(b.entries, e)
		}
	}

	if config.StartEnabled.Load() {
		b.enabler.Enable()
	}

	b.rpcServer = pb.NewBasicBoardServer(&server{board: b},
		twirp.WithServerPathPrefix("/tv"),
		twirp.ChainHooks(twirphelpers.GetDefaultHooks(b, logger)),
	)

	if err := util.SetCrons(config.OnTimes, func() {
		logger.Info("tv board turning on")
		b.enabler.Enable()
	}); err != nil {
		return nil, err
	}
	if err := util.SetCrons(config.OffTimes, func() {
		logger.Info("tv board turning off")
		b.enabler.Disable()
	}); err != nil {
		return nil, err
	}

	return b, nil
}

// Name ...
func (b *Board) Name() string { return Name }

// Enabler ...
func (b *Board) Enabler() board.Enabler { return b.enabler }

// InBetween ...
func (b *Board) InBetween() bool { return false }

// GetHTTPHandlers ...
func (b *Board) GetHTTPHandlers() ([]*board.HTTPHandler, error) { return nil, nil }

// GetRPCHandler ...
func (b *Board) GetRPCHandler() (string, http.Handler) {
	return b.rpcServer.PathPrefix(), b.rpcServer
}

// BoardDelay is how long each episode shows for.
func (b *Board) BoardDelay() time.Duration { return b.config.boardDelay.Load() }

// SetBoardDelay changes how long each episode shows for.
func (b *Board) SetBoardDelay(d time.Duration) { b.config.boardDelay.Store(d) }

func (b *Board) lookahead() time.Duration {
	return time.Duration(b.config.LookaheadDays) * 24 * time.Hour
}

// upcoming are the new episodes still to air, or on now, within the lookahead,
// soonest first. It asks TVmaze again when what it has is old.
func (b *Board) upcoming(ctx context.Context) []tvmaze.Episode {
	b.lock.Lock()
	defer b.lock.Unlock()

	now := b.now()
	stale := b.fetched.IsZero() || now.Sub(b.fetched) >= refreshEvery
	waiting := !b.lastFail.IsZero() && now.Sub(b.lastFail) < failRetry
	if stale && !waiting && len(b.entries) > 0 {
		b.refreshLocked(ctx, now)
	}
	if !b.fetched.IsZero() && now.Sub(b.fetched) > staleLimit {
		return nil
	}

	var out []tvmaze.Episode
	for _, e := range b.episodes {
		end := e.Airs.Add(max(e.Runtime, time.Hour))
		if !e.Timed {
			// a streaming drop is up all day
			end = e.Airs.Add(24 * time.Hour)
		}
		if end.After(now) && !e.Airs.After(now.Add(b.lookahead())) {
			out = append(out, e)
		}
	}
	return out
}

// refreshLocked asks TVmaze about every show. A show it can't find is left
// out, and the rest still show. The caller holds b.lock.
func (b *Board) refreshLocked(ctx context.Context, now time.Time) {
	from := now.Add(-6 * time.Hour)
	// to the end of the lookahead as of the next refresh, so an episode
	// doesn't wait a refresh to appear
	to := now.Add(b.lookahead() + refreshEvery)

	var eps []tvmaze.Episode
	var problems []string
	failed := 0
	for _, e := range b.entries {
		id, err := b.idLocked(ctx, e)
		if err != nil {
			problems = append(problems, err.Error())
			failed++
			continue
		}
		show, found, err := b.api.Upcoming(ctx, id, from, to)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %s", e.Name, err))
			failed++
			continue
		}
		b.shows[id] = show
		eps = append(eps, found...)
	}

	problem := strings.Join(problems, "; ")
	if problem != b.problem && problem != "" {
		b.log.Warn("tv board couldn't check every show", zap.String("problems", problem))
	}
	b.problem = problem

	if failed == len(b.entries) {
		// nothing worked: keep what there was, and try again in a while
		b.lastFail = now
		return
	}

	sort.SliceStable(eps, func(i, j int) bool { return eps[i].Airs.Before(eps[j].Airs) })
	b.episodes = eps
	b.fetched = now
	b.lastFail = time.Time{}
}

// idLocked is an entry's TVmaze ID, looking it up by name, once, for one
// typed into the config file without one.
func (b *Board) idLocked(ctx context.Context, e tvmaze.Entry) (int, error) {
	if e.ID != 0 {
		return e.ID, nil
	}
	if id, ok := b.found[strings.ToLower(e.Name)]; ok {
		return id, nil
	}
	show, err := b.api.Find(ctx, e.Name)
	if err != nil {
		return 0, err
	}
	b.log.Info("tv board looked up a show by name",
		zap.String("name", e.Name),
		zap.String("found", show.Name),
		zap.Int("tvmaze id", show.ID),
	)
	b.found[strings.ToLower(e.Name)] = show.ID
	b.shows[show.ID] = show
	return show.ID, nil
}

// Shows are the shows the board follows, with their networks when the board
// has asked TVmaze about them.
func (b *Board) Shows(ctx context.Context) []board.Show {
	b.lock.Lock()
	defer b.lock.Unlock()

	out := make([]board.Show, 0, len(b.entries))
	for _, e := range b.entries {
		id := e.ID
		if id == 0 {
			id = b.found[strings.ToLower(e.Name)]
		}
		s := board.Show{ID: id, Name: e.Name}
		if id != 0 {
			show, ok := b.shows[id]
			if !ok {
				var err error
				show, _, err = b.api.Show(ctx, id)
				if err == nil {
					b.shows[id] = show
					ok = true
				}
			}
			if ok {
				s.Network, s.Premiered = show.Network, show.Premiered
			}
		}
		out = append(out, s)
	}
	return out
}

// ErrBadShow is a show that can't be followed.
var ErrBadShow = errors.New("that show can't be followed")

type showError struct {
	err error
}

func (e *showError) Error() string   { return e.err.Error() }
func (e *showError) Unwrap() []error { return []error{ErrBadShow, e.err} }

// SetShows changes the shows the board follows. A show it doesn't follow yet
// is looked up on TVmaze first, so only real ones are saved, under the names
// TVmaze gives them.
func (b *Board) SetShows(ctx context.Context, shows []board.Show) ([]string, error) {
	b.lock.Lock()
	known := make(map[int]tvmaze.Entry, len(b.entries))
	for _, e := range b.entries {
		if e.ID != 0 {
			known[e.ID] = e
		}
	}
	b.lock.Unlock()

	var entries []tvmaze.Entry
	seen := make(map[int]bool)
	for _, s := range shows {
		if s.ID <= 0 {
			return nil, &showError{err: fmt.Errorf("%q has no TVmaze ID; search for it and pick it from the list", s.Name)}
		}
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true

		if e, ok := known[s.ID]; ok {
			entries = append(entries, e)
			continue
		}
		show, _, err := b.api.Show(ctx, s.ID)
		if err != nil {
			if errors.Is(err, tvmaze.ErrNotFound) {
				return nil, &showError{err: err}
			}
			return nil, fmt.Errorf("couldn't check %q with TVmaze: %w", s.Name, err)
		}
		entries = append(entries, tvmaze.Entry{Name: show.Name, ID: show.ID})

		b.lock.Lock()
		b.shows[show.ID] = show
		b.lock.Unlock()
	}

	saved := make([]string, 0, len(entries))
	for _, e := range entries {
		saved = append(saved, e.String())
	}

	b.lock.Lock()
	b.entries = entries
	b.config.Shows = saved
	// ask about the new list next time round
	b.fetched = time.Time{}
	b.lastFail = time.Time{}
	b.episodes = nil
	b.lock.Unlock()

	return saved, nil
}

// SearchShows finds shows on TVmaze by name.
func (b *Board) SearchShows(ctx context.Context, query string) ([]board.Show, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, &showError{err: errors.New("type a show's name to search for it")}
	}
	found, err := b.api.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]board.Show, 0, len(found))
	for _, s := range found {
		out = append(out, board.Show{ID: s.ID, Name: s.Name, Network: s.Network, Premiered: s.Premiered})
		if len(out) == 8 {
			break
		}
	}
	return out, nil
}
