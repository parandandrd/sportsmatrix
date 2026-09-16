package sportsmatrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/conffile"
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
