package sportsmatrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	sportboard "github.com/parandandrd/sportsmatrix/internal/board/sport"
	"github.com/parandandrd/sportsmatrix/internal/conffile"
	pb "github.com/parandandrd/sportsmatrix/internal/proto/sportsmatrix"
)

// SetConfigFile gives the matrix the config file it was started from, so that
// what gets changed through the API is written back to it. Without one,
// changes last until the service restarts, as they always had.
func (s *SportsMatrix) SetConfigFile(f *conffile.File) {
	s.settingsLock.Lock()
	defer s.settingsLock.Unlock()
	s.configFile = f
}

// boardKey is where a board's own settings live in the config file: its
// section, or for a league's stats and headlines boards, the block of that
// name inside the league's section. Nil for a board with no known section.
func (s *SportsMatrix) boardKey(b board.Board) []string {
	s.Lock()
	sections := s.boardSections
	s.Unlock()

	section := sections[b]
	if section == "" {
		return nil
	}

	sub := subKey(b)
	if sub == "" {
		return []string{section}
	}

	// PGA's stats board is the whole of its section, not a part of a league's
	for other, otherSection := range sections {
		if other != b && otherSection == section && subKey(other) == "" {
			return []string{section, sub}
		}
	}

	return []string{section}
}

func subKey(b board.Board) string {
	path, _ := b.GetRPCHandler()
	switch {
	case strings.HasPrefix(path, "/stat/"):
		return "stats"
	case strings.HasPrefix(path, "/headlines/"):
		return "headlines"
	case strings.HasPrefix(path, "/forecast/"):
		return "forecast"
	default:
		return ""
	}
}

// boardEdit is an edit to one of b's settings, or nil if b has no place in the
// config file.
func (s *SportsMatrix) boardEdit(b board.Board, setting string, value any) *conffile.Edit {
	key := s.boardKey(b)
	if key == nil {
		return nil
	}
	return &conffile.Edit{
		Path:  append(append([]string(nil), key...), setting),
		Value: value,
	}
}

// save writes edits to the config file, if there is one. Callers hold
// settingsLock across both changing a setting and saving it, so that two
// changes to the same setting reach the file in the order they were made.
func (s *SportsMatrix) save(edits ...conffile.Edit) error {
	if s.configFile == nil || len(edits) == 0 {
		return nil
	}

	changed, err := s.configFile.Apply(edits...)
	if err != nil {
		s.log.Error("failed to save settings to the config file",
			zap.String("file", s.configFile.Path()),
			zap.Error(err),
		)
		return fmt.Errorf("could not save that to %s: %w", s.configFile.Path(), err)
	}

	if changed {
		s.refreshSectionOrder()
	}

	return nil
}

// refreshSectionOrder rereads which sections the config file has and in what
// order, after the file has been written to.
func (s *SportsMatrix) refreshSectionOrder() {
	keys, err := s.configFile.Sections()
	if err != nil {
		s.log.Error("failed to read config file sections", zap.Error(err))
		return
	}

	order := sectionPositions(keys)

	s.Lock()
	defer s.Unlock()
	s.sectionOrder = order
	s.sortBoards()
}

// errNoConfigFile is returned for a change that only makes sense saved.
var errNoConfigFile = errors.New("this matrix was started without a config file, so there is nowhere to keep that")

// setBoardOrder rearranges boards by their config sections: see SetBoardOrder
// in the proto. A section the file doesn't have is added to it first, so that
// it has a place to be moved from.
func (s *SportsMatrix) setBoardOrder(sections []string) error {
	s.settingsLock.Lock()
	defer s.settingsLock.Unlock()

	if s.configFile == nil {
		return errNoConfigFile
	}

	s.Lock()
	main := make(map[string]board.Board)
	for _, b := range append(append([]board.Board(nil), s.boards...), s.betweenBoards...) {
		section := strings.ToLower(s.boardSections[b])
		if section == "" {
			continue
		}
		if current, ok := main[section]; !ok || (subKey(current) != "" && subKey(b) == "") {
			main[section] = b
		}
	}
	inFile := s.sectionOrder
	s.Unlock()

	var add []conffile.Edit
	for _, section := range sections {
		b, ok := main[strings.ToLower(section)]
		if !ok {
			return fmt.Errorf("%w: no board comes from a config section called %q", errUnknownSection, section)
		}
		if _, ok := inFile[strings.ToLower(section)]; !ok {
			if e := s.boardEdit(b, "enabled", b.Enabler().Enabled()); e != nil {
				add = append(add, *e)
			}
		}
	}

	if err := s.save(add...); err != nil {
		return err
	}

	changed, err := s.configFile.Order(sections)
	if err != nil {
		return fmt.Errorf("could not save the new order to %s: %w", s.configFile.Path(), err)
	}
	if changed {
		s.refreshSectionOrder()

		var order []string
		for _, b := range s.boardList() {
			order = append(order, b.Name())
		}
		s.log.Info("Board Render order changed", zap.Strings("order", order))
	}

	return nil
}

var (
	errUnknownBoard   = errors.New("no such board")
	errNoSuchSetting  = errors.New("that board doesn't have that setting")
	errBadDelay       = errors.New("not a display time that board can have")
	errUnknownSection = errors.New("unknown section")
	errBadSchedule    = errors.New("not a cron schedule")
	errBadBrightness  = errors.New("brightness goes from 1 to 100")
	errBadLocation    = errors.New("not a location that board can show")
)

// settings are the matrix-wide settings as they are now.
func (s *SportsMatrix) settings() *pb.Settings {
	s.settingsLock.Lock()
	defer s.settingsLock.Unlock()

	out := &pb.Settings{
		Brightness: int32(s.cfg.HardwareConfig.Brightness),
		ScreenSchedule: &pb.ScreenSchedule{
			OnTimes:  append([]string{}, s.cfg.ScreenOnTimes...),
			OffTimes: append([]string{}, s.cfg.ScreenOffTimes...),
		},
	}
	if s.configFile != nil {
		out.ConfigFile = s.configFile.Path()
	}

	return out
}

// brightnessSetter is a canvas that drives real LEDs.
type brightnessSetter interface {
	SetBrightness(int)
}

func (s *SportsMatrix) setBrightness(brightness int) error {
	if brightness < 1 || brightness > 100 {
		return errBadBrightness
	}

	s.settingsLock.Lock()
	defer s.settingsLock.Unlock()

	for _, c := range s.canvases {
		if b, ok := c.(brightnessSetter); ok {
			b.SetBrightness(brightness)
		}
	}
	s.cfg.HardwareConfig.Brightness = brightness

	return s.save(conffile.Edit{
		Path:  []string{"sportsMatrixConfig", "hardwareConfig", "brightness"},
		Value: brightness,
	})
}

func (s *SportsMatrix) setScreenSchedule(on, off []string) error {
	s.settingsLock.Lock()
	defer s.settingsLock.Unlock()

	if on == nil {
		on = []string{}
	}
	if off == nil {
		off = []string{}
	}

	if err := s.scheduleScreen(on, off); err != nil {
		return err
	}

	return s.save(
		conffile.Edit{Path: []string{"sportsMatrixConfig", "screenOnTimes"}, Value: on},
		conffile.Edit{Path: []string{"sportsMatrixConfig", "screenOffTimes"}, Value: off},
	)
}

// setEnabled turns boards on or off and saves each one whose state changed.
// A board already in the state asked for leaves its config alone, so turning
// every board off does not add sections to the file for boards it never had.
func (s *SportsMatrix) setEnabled(boards []board.Board, enabled bool) error {
	s.settingsLock.Lock()
	defer s.settingsLock.Unlock()

	var edits []conffile.Edit
	for _, b := range boards {
		if !b.Enabler().Store(enabled) {
			continue
		}
		if e := s.boardEdit(b, "enabled", enabled); e != nil {
			edits = append(edits, *e)
		}
	}

	return s.save(edits...)
}

// statusSettings maps each board service's status fields, as its JSON names
// them, to the config keys they are read from. The detailed_live and
// odds_enabled fields of the sport service are not here because nothing on
// the server handles them.
var statusSettings = map[string]map[string]string{
	"sport.v1.Sport": {
		"enabled":             "enabled",
		"favorite_hidden":     "hideFavoriteScore",
		"favorite_sticky":     "favoriteSticky",
		"record_rank_enabled": "showRecord",
		"use_gradient":        "useGradient",
		"live_only":           "liveOnly",
		"show_league_logo":    "showLeagueLogo",
	},
	"board.v1.BasicBoard": {
		"enabled": "enabled",
	},
	"racing.v1.Racing": {
		"enabled": "enabled",
	},
	"imageboard.v1.ImageBoard": {
		"enabled":           "enabled",
		"diskcache_enabled": "useDiskCache",
		"memcache_enabled":  "useMemCache",
	},
}

// savingHandler wraps the RPC service a board mounts so that what its
// SetStatus changes is saved to the config file. It compares the board's status
// before and after rather than reading the request, so that only what actually
// changed is written: the web UI sends every field every time.
func (s *SportsMatrix) savingHandler(b board.Board, prefix string, h http.Handler) http.Handler {
	parts := strings.Split(strings.Trim(prefix, "/"), "/")
	settings := statusSettings[parts[len(parts)-1]]
	if settings == nil {
		return h
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.URL.Path, "/SetStatus") {
			h.ServeHTTP(w, req)
			return
		}

		s.settingsLock.Lock()
		defer s.settingsLock.Unlock()

		before, beforeErr := boardStatus(req.Context(), h, prefix)

		rec := newRecorder()
		h.ServeHTTP(rec, req)
		if rec.code != http.StatusOK || beforeErr != nil || s.configFile == nil {
			rec.copyTo(w)
			return
		}

		after, err := boardStatus(req.Context(), h, prefix)
		if err != nil {
			s.log.Error("failed to read a board's status after setting it", zap.String("board", b.Name()), zap.Error(err))
			rec.copyTo(w)
			return
		}

		var edits []conffile.Edit
		for field, setting := range settings {
			now, ok := after[field].(bool)
			if !ok || before[field] == after[field] {
				continue
			}
			if e := s.boardEdit(b, setting, now); e != nil {
				edits = append(edits, *e)
			}
		}

		if err := s.save(edits...); err != nil {
			writeTwirpError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}

		rec.copyTo(w)
	})
}

// boardStatus asks a board's own service for its status, in process.
func boardStatus(ctx context.Context, h http.Handler, prefix string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, prefix+"GetStatus", strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	rec := newRecorder()
	h.ServeHTTP(rec, req)
	if rec.code != http.StatusOK {
		return nil, fmt.Errorf("GetStatus answered %d: %s", rec.code, rec.body.String())
	}

	var resp struct {
		Status map[string]any `json:"status"`
	}
	if err := json.Unmarshal(rec.body.Bytes(), &resp); err != nil {
		return nil, err
	}

	return resp.Status, nil
}

func writeTwirpError(w http.ResponseWriter, status int, code string, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "msg": msg})
}

// recorder holds a response so it can be looked at before it is sent.
type recorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func newRecorder() *recorder {
	return &recorder{header: http.Header{}, code: http.StatusOK}
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(p []byte) (int, error) { return r.body.Write(p) }
func (r *recorder) WriteHeader(code int)        { r.code = code }

func (r *recorder) copyTo(w http.ResponseWriter) {
	for k, v := range r.header {
		w.Header()[k] = v
	}
	w.WriteHeader(r.code)
	_, _ = w.Write(r.body.Bytes())
}

// findBoard is the board with a name, matched as Jump matches it.
func (s *SportsMatrix) findBoard(name string) board.Board {
	s.Lock()
	defer s.Unlock()

	for _, group := range [][]board.Board{s.boards, s.betweenBoards} {
		for _, b := range group {
			if strings.EqualFold(b.Name(), name) {
				return b
			}
		}
	}

	return nil
}

// defaultMinDelay is the least time a board without a floor of its own can
// be shown for.
const defaultMinDelay = 5 * time.Second

func minDelay(b board.Board) time.Duration {
	if m, ok := b.(board.DelayMinimum); ok {
		return m.MinBoardDelay()
	}
	return defaultMinDelay
}

// formatDelay writes a display time the way people write them in the config
// file: "10s", "1m30s", "2m" rather than time.Duration's "2m0s".
func formatDelay(d time.Duration) string {
	minutes, seconds := d/time.Minute, (d % time.Minute).Seconds()
	switch {
	case minutes == 0:
		return strconv.FormatFloat(seconds, 'f', -1, 64) + "s"
	case seconds == 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return fmt.Sprintf("%dm%ss", minutes, strconv.FormatFloat(seconds, 'f', -1, 64))
	}
}

func (s *SportsMatrix) boardSettings(ctx context.Context, name string) (*pb.BoardSettings, error) {
	b := s.findBoard(name)
	if b == nil {
		return nil, fmt.Errorf("%w called %q", errUnknownBoard, name)
	}

	out := &pb.BoardSettings{Name: b.Name()}

	if d, ok := b.(board.DelaySetter); ok {
		out.HasBoardDelay = true
		out.BoardDelay = formatDelay(d.BoardDelay())
		out.MinBoardDelay = formatDelay(minDelay(b))
	}

	if sb, ok := b.(*sportboard.SportBoard); ok {
		out.HasTeams = true
		out.WatchTeams, out.FavoriteTeams = sb.Teams()

		// The teams are the league's own, fetched and kept by the board's API,
		// so a slow answer here is not worth failing the rest over.
		tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		teams, err := sb.TeamChoices(tctx)
		if err != nil {
			s.log.Warn("failed to list teams for board settings", zap.String("board", b.Name()), zap.Error(err))
		}
		// ESPN's leagues list their teams from more than one endpoint, and the
		// same team comes back from each
		seen := make(map[string]bool, len(teams))
		for _, t := range teams {
			if seen[t.GetAbbreviation()] {
				continue
			}
			seen[t.GetAbbreviation()] = true
			out.Teams = append(out.Teams, &pb.Team{Abbreviation: t.GetAbbreviation(), Name: t.GetDisplayName()})
		}
		sort.Slice(out.Teams, func(i, j int) bool { return out.Teams[i].Name < out.Teams[j].Name })
	}

	if l, ok := b.(board.LocationSetter); ok {
		out.HasLocation = true
		// the place name comes with the weather, which can take a fetch
		lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		out.Location, out.LocationPlace = l.Location(lctx)
	}

	return out, nil
}

func (s *SportsMatrix) setBoardSettings(ctx context.Context, req *pb.BoardSettings) error {
	b := s.findBoard(req.Name)
	if b == nil {
		return fmt.Errorf("%w called %q", errUnknownBoard, req.Name)
	}

	s.settingsLock.Lock()
	defer s.settingsLock.Unlock()

	var edits []conffile.Edit

	if req.BoardDelay != "" {
		d, ok := b.(board.DelaySetter)
		if !ok {
			return fmt.Errorf("%w: %s has no display time", errNoSuchSetting, b.Name())
		}
		delay, err := time.ParseDuration(req.BoardDelay)
		if err != nil || delay < minDelay(b) || delay > time.Hour {
			return fmt.Errorf("%w: %q for %s, which takes from %s to 1h", errBadDelay, req.BoardDelay, b.Name(), formatDelay(minDelay(b)))
		}
		if delay != d.BoardDelay() {
			d.SetBoardDelay(delay)
			if e := s.boardEdit(b, "boardDelay", formatDelay(delay)); e != nil {
				edits = append(edits, *e)
			}
		}
	}

	if req.HasTeams {
		sb, ok := b.(*sportboard.SportBoard)
		if !ok {
			return fmt.Errorf("%w: %s has no teams", errNoSuchSetting, b.Name())
		}
		watchBefore, favoriteBefore := sb.Teams()
		if sb.SetTeams(teamList(req.WatchTeams), teamList(req.FavoriteTeams)) {
			watch, favorite := sb.Teams()
			if !slices.Equal(watch, watchBefore) {
				if e := s.boardEdit(b, "watchTeams", watch); e != nil {
					edits = append(edits, *e)
				}
			}
			if !slices.Equal(favorite, favoriteBefore) {
				if e := s.boardEdit(b, "favoriteTeams", favorite); e != nil {
					edits = append(edits, *e)
				}
			}
		}
	}

	if req.HasLocation {
		l, ok := b.(board.LocationSetter)
		if !ok {
			return fmt.Errorf("%w: %s has no location", errNoSuchSetting, b.Name())
		}
		if strings.TrimSpace(req.Location) == "" {
			return fmt.Errorf("%w: enter a latitude and longitude, like 41.8858, -87.6181", errBadLocation)
		}
		// checking the location asks the weather provider, which can be slow
		lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		saved, err := l.SetLocation(lctx, req.Location)
		if err != nil {
			return &locationError{err: err}
		}
		if e := s.boardEdit(b, "location", saved); e != nil {
			edits = append(edits, *e)
		}
	}

	return s.save(edits...)
}

// locationError is a location a board refused. Its message is the board's own,
// which is written for whoever typed the location.
type locationError struct {
	err error
}

func (e *locationError) Error() string   { return e.err.Error() }
func (e *locationError) Unwrap() []error { return []error{errBadLocation, e.err} }

// teamList tidies a list of teams as typed: abbreviations are matched as the
// league writes them, which is in capitals.
func teamList(teams []string) []string {
	out := []string{}
	for _, t := range teams {
		t = strings.ToUpper(strings.TrimSpace(t))
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}
