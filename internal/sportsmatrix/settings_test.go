package sportsmatrix

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twitchtv/twirp"
	"go.uber.org/atomic"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	yamlv2 "gopkg.in/yaml.v2"

	"github.com/parandandrd/sportsmatrix/internal/board"
	calendarboard "github.com/parandandrd/sportsmatrix/internal/board/calendar"
	"github.com/parandandrd/sportsmatrix/internal/board/clock"
	imageboard "github.com/parandandrd/sportsmatrix/internal/board/image"
	racingboard "github.com/parandandrd/sportsmatrix/internal/board/racing"
	sportboard "github.com/parandandrd/sportsmatrix/internal/board/sport"
	statboard "github.com/parandandrd/sportsmatrix/internal/board/stat"
	sysboard "github.com/parandandrd/sportsmatrix/internal/board/sys"
	textboard "github.com/parandandrd/sportsmatrix/internal/board/text"
	"github.com/parandandrd/sportsmatrix/internal/conffile"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
	pb "github.com/parandandrd/sportsmatrix/internal/proto/sportsmatrix"
	rgb "github.com/parandandrd/sportsmatrix/internal/rgbmatrix-rpi"
)

// pathBoard is a board that mounts a service at a given path, which is all
// that tells a league's stats and headlines boards from the league's own.
type pathBoard struct {
	*TestBoard
	name string
	path string
}

func (b *pathBoard) Name() string { return b.name }

func (b *pathBoard) GetRPCHandler() (string, http.Handler) {
	return b.path, http.NotFoundHandler()
}

func TestBoardKey(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.ErrorLevel))
	mk := func(name, path string) *pathBoard {
		return &pathBoard{TestBoard: &TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()}, name: name, path: path}
	}

	nhl := mk("NHL", "/nhl/sport.v1.Sport/")
	nhlStats := mk("StatBoard: NHL", "/stat/nhl/board.v1.BasicBoard/")
	nhlHeadlines := mk("NHL Headlines", "/headlines/nhl/board.v1.BasicBoard/")
	pga := mk("StatBoard: PGA", "/stat/pga/board.v1.BasicBoard/")
	clockBoard := mk("Clock", "/clock/board.v1.BasicBoard/")
	stray := mk("Stray", "")

	s := &SportsMatrix{}
	s.SetBoardSections(map[board.Board]string{
		nhl: "nhlConfig", nhlStats: "nhlConfig", nhlHeadlines: "nhlConfig",
		pga: "pga", clockBoard: "clockConfig",
	}, nil)

	require.Equal(t, []string{"nhlConfig"}, s.boardKey(nhl))
	require.Equal(t, []string{"nhlConfig", "stats"}, s.boardKey(nhlStats))
	require.Equal(t, []string{"nhlConfig", "headlines"}, s.boardKey(nhlHeadlines))
	// PGA's stats board is its whole section, not a part of a league's
	require.Equal(t, []string{"pga"}, s.boardKey(pga))
	require.Equal(t, []string{"clockConfig"}, s.boardKey(clockBoard))
	require.Nil(t, s.boardKey(stray))
}

// The settings a board's own service changes are saved under the keys the
// config loader reads them from, or they would come back wrong after a restart.
func TestStatusSettingsAreConfigKeys(t *testing.T) {
	t.Parallel()

	tags := func(v any) map[string]bool {
		out := map[string]bool{}
		ty := reflect.TypeOf(v)
		for i := 0; i < ty.NumField(); i++ {
			name, _, _ := strings.Cut(ty.Field(i).Tag.Get("json"), ",")
			out[name] = true
		}
		return out
	}

	configs := map[string][]any{
		"sport.v1.Sport":           {sportboard.Config{}},
		"board.v1.BasicBoard":      {clock.Config{}, sysboard.Config{}, textboard.Config{}, statboard.Config{}, calendarboard.Config{}},
		"racing.v1.Racing":         {racingboard.Config{}},
		"imageboard.v1.ImageBoard": {imageboard.Config{}},
	}

	require.Len(t, statusSettings, len(configs))
	for service, settings := range statusSettings {
		require.NotEmpty(t, configs[service], service)
		for _, key := range settings {
			for _, c := range configs[service] {
				require.True(t, tags(c)[key], "%T has no %q to save %s's status to", c, key, service)
			}
		}
	}
}

// nolint: paralleltest // binds a port
func TestSettingsAreSavedToTheConfigFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))

	dat, err := os.ReadFile("../../sportsmatrix.conf.example")
	require.NoError(t, err)
	dir := t.TempDir()
	path := filepath.Join(dir, "sportsmatrix.conf")
	require.NoError(t, os.WriteFile(path, dat, 0o644))
	file := conffile.New(path)

	cfg := &Config{ServeWebUI: false, HTTPListenPort: 18204, WebBoardWidth: 1}
	cfg.Defaults()
	canvas := board.NewBlankCanvas(1, 1, logger)
	canvas.Enable()

	mk := func(name, path string, on bool) *pathBoard {
		b := &pathBoard{TestBoard: &TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()}, name: name, path: path}
		b.enabler.Store(on)
		return b
	}

	// as the example config has them
	nhl := mk("NHL", "/nhl/sport.v1.Sport/", true)
	mlb := mk("MLB", "/mlb/sport.v1.Sport/", true)
	uefa := mk("UEFA", "/uefa/sport.v1.Sport/", false)
	nhlHeadlines := newHeadlines(t, logger, "nhl")

	s, err := New(ctx, logger, cfg, []board.Canvas{canvas}, nhl, nhlHeadlines, mlb, uefa)
	require.NoError(t, err)
	defer s.Close()

	sections, err := file.Sections()
	require.NoError(t, err)
	s.SetBoardSections(map[board.Board]string{
		nhl: "nhlConfig", nhlHeadlines: "nhlConfig", mlb: "mlbConfig", uefa: "uefaConfig",
	}, sections)
	s.SetConfigFile(file)

	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = s.Serve(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-served:
		case <-time.After(10 * time.Second):
			t.Error("timed out waiting for Serve to return")
		}
	})
	select {
	case <-s.isServing:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the matrix to serve")
	}

	post := func(path, body string) (int, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost:18204"+path, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(out)
	}
	saved := func(keys ...string) any {
		t.Helper()
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var cur any
		require.NoError(t, yamlv2.Unmarshal(raw, &cur))
		for _, k := range keys {
			m, ok := cur.(map[any]any)
			if !ok {
				return nil
			}
			cur = m[k]
		}
		return cur
	}

	code, body := post("/matrix.v1.Sportsmatrix/SetBoardEnabled", `{"name":"NHL","enabled":false}`)
	require.Equal(t, http.StatusOK, code, body)
	require.Equal(t, false, saved("nhlConfig", "enabled"))

	// a board's own service, as the settings panels use it
	code, body = post("/headlines/nhl/board.v1.BasicBoard/SetStatus", `{"status":{"enabled":true}}`)
	require.Equal(t, http.StatusOK, code, body)
	require.Equal(t, true, saved("nhlConfig", "headlines", "enabled"))

	// Turning everything off saves the boards it turned off, and leaves the
	// ones that were already off -- UEFA has no section -- out of the file.
	code, body = post("/matrix.v1.Sportsmatrix/SetAll", `{"enabled":false}`)
	require.Equal(t, http.StatusOK, code, body)
	require.Equal(t, false, saved("mlbConfig", "enabled"))
	require.Equal(t, false, saved("nhlConfig", "headlines", "enabled"))
	require.Nil(t, saved("uefaConfig"))

	// Turning on a board the file doesn't have gives it a section, and then
	// ListBoards knows it is in the file.
	code, body = post("/matrix.v1.Sportsmatrix/SetBoardEnabled", `{"name":"UEFA","enabled":true}`)
	require.Equal(t, http.StatusOK, code, body)
	require.Equal(t, true, saved("uefaConfig", "enabled"))

	code, body = post("/matrix.v1.Sportsmatrix/ListBoards", "{}")
	require.Equal(t, http.StatusOK, code, body)
	var list struct {
		Boards []struct {
			Name         string `json:"name"`
			InConfigFile bool   `json:"in_config_file"`
		} `json:"boards"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &list))
	for _, b := range list.Boards {
		require.True(t, b.InConfigFile, "%s should be in the config file now", b.Name)
	}

	// Everything else in the file is where it was: the new section is on the
	// end, and the rest has as many lines as the example did.
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	appended := "\n\nuefaConfig:\n  enabled: true\n"
	require.True(t, strings.HasSuffix(string(after), appended))
	require.Len(t, strings.Split(strings.TrimSuffix(string(after), appended), "\n"), len(strings.Split(string(dat), "\n")))

	if os.Geteuid() != 0 {
		// When the file can't be written, the change still happens, and the
		// caller is told it didn't stick.
		require.NoError(t, os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		code, body = post("/matrix.v1.Sportsmatrix/SetBoardEnabled", `{"name":"MLB","enabled":true}`)
		require.Equal(t, http.StatusInternalServerError, code, body)
		require.Contains(t, body, "could not save that to "+path)
		require.True(t, mlb.enabler.Enabled())
	}
}

// The panel cycles through boards in the config file's order, and moving them
// moves the file's sections, so the order outlasts a restart.
func TestBoardOrder(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))
	mk := func(name, path string, on bool) *pathBoard {
		b := &pathBoard{TestBoard: &TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()}, name: name, path: path}
		b.enabler.Store(on)
		return b
	}

	nhl := mk("NHL", "/nhl/sport.v1.Sport/", true)
	nhlHeadlines := mk("NHL Headlines", "/headlines/nhl/board.v1.BasicBoard/", false)
	mlb := mk("MLB", "/mlb/sport.v1.Sport/", true)
	clockBoard := mk("Clock", "/clock/board.v1.BasicBoard/", true)
	uefa := mk("UEFA", "/uefa/sport.v1.Sport/", false)

	dat, err := os.ReadFile("../../sportsmatrix.conf.example")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "sportsmatrix.conf")
	require.NoError(t, os.WriteFile(path, dat, 0o644))
	file := conffile.New(path)

	sections, err := file.Sections()
	require.NoError(t, err)

	// built in getBoards' order, which is not the file's
	s := &SportsMatrix{log: logger, boards: []board.Board{nhl, nhlHeadlines, mlb, uefa, clockBoard}}
	s.SetBoardSections(map[board.Board]string{
		nhl: "nhlConfig", nhlHeadlines: "nhlConfig", mlb: "mlbConfig", uefa: "uefaConfig", clockBoard: "clockConfig",
	}, sections)
	s.SetConfigFile(file)

	names := func() []string {
		var out []string
		for _, b := range s.boardList() {
			out = append(out, b.Name())
		}
		return out
	}
	require.Equal(t, []string{"Clock", "NHL", "NHL Headlines", "MLB", "UEFA"}, names(),
		"the file has clock, then NHL, then MLB, and no UEFA")

	svr := &Server{sm: s}

	_, err = svr.SetBoardOrder(context.Background(), &pb.SetBoardOrderReq{Sections: []string{"mlbConfig", "clockConfig", "nhlConfig"}})
	require.NoError(t, err)
	require.Equal(t, []string{"MLB", "Clock", "NHL", "NHL Headlines", "UEFA"}, names())

	keys, err := file.Sections()
	require.NoError(t, err)
	require.Equal(t, []string{"sportsMatrixConfig", "mlbConfig", "sysConfig", "ncaafConfig", "clockConfig", "nhlConfig"}, keys[:6])

	// a board with no section gets one, and then goes where it was put
	_, err = svr.SetBoardOrder(context.Background(), &pb.SetBoardOrderReq{Sections: []string{"uefaConfig", "mlbConfig"}})
	require.NoError(t, err)
	require.Equal(t, []string{"UEFA", "Clock", "NHL", "NHL Headlines", "MLB"}, names())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(after), "\nuefaConfig:\n  enabled: false\n")

	var twerr twirp.Error
	_, err = svr.SetBoardOrder(context.Background(), &pb.SetBoardOrderReq{Sections: []string{"nhlConfig", "stocksConfig"}})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())

	noFile := &SportsMatrix{log: logger, boards: []board.Board{nhl}}
	noFile.SetBoardSections(map[board.Board]string{nhl: "nhlConfig"}, nil)
	_, err = (&Server{sm: noFile}).SetBoardOrder(context.Background(), &pb.SetBoardOrderReq{Sections: []string{"nhlConfig"}})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.FailedPrecondition, twerr.Code())
}

// brightCanvas is a canvas that drives LEDs, as far as brightness goes.
type brightCanvas struct {
	board.Canvas
	brightness int
}

func (c *brightCanvas) SetBrightness(b int) { c.brightness = b }

func TestBrightnessAndScreenSchedule(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))

	dat, err := os.ReadFile("../../sportsmatrix.conf.example")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "sportsmatrix.conf")
	require.NoError(t, os.WriteFile(path, dat, 0o644))

	cfg := &Config{
		WebBoardWidth:  1,
		ScreenOnTimes:  []string{"0 19 * * *"},
		ScreenOffTimes: []string{"0 0 * * *"},
		HardwareConfig: &rgb.HardwareConfig{Brightness: 60},
	}
	leds := &brightCanvas{Canvas: board.NewBlankCanvas(1, 1, logger)}
	b := &TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()}

	s, err := New(ctx, logger, cfg, []board.Canvas{leds}, b)
	require.NoError(t, err)
	s.SetConfigFile(conffile.New(path))
	require.Len(t, s.cron.Entries(), 2)

	svr := &Server{sm: s}
	saved := func() string {
		t.Helper()
		out, err := os.ReadFile(path)
		require.NoError(t, err)
		return string(out)
	}

	_, err = svr.SetBrightness(ctx, &pb.SetBrightnessReq{Brightness: 35})
	require.NoError(t, err)
	require.Equal(t, 35, leds.brightness, "straight to the panel")
	require.Contains(t, saved(), "  hardwareConfig:\n    cols: 64\n    rows: 32\n\n    # 1 to 100. Make sure your power supply is sufficient\n    brightness: 35\n")

	var twerr twirp.Error
	_, err = svr.SetBrightness(ctx, &pb.SetBrightnessReq{Brightness: 0})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())
	require.Equal(t, 35, leds.brightness)

	_, err = svr.SetScreenSchedule(ctx, &pb.ScreenSchedule{
		OnTimes:  []string{"30 7 * * *"},
		OffTimes: []string{"0 23 * * 0-4", "@midnight"},
	})
	require.NoError(t, err)
	require.Len(t, s.cron.Entries(), 3, "the old jobs are replaced, not added to")
	require.Contains(t, saved(), "  screenOffTimes:\n  - \"0 23 * * 0-4\"\n  - \"@midnight\"\n")
	require.Contains(t, saved(), "  screenOnTimes:\n  - \"30 7 * * *\"\n")

	before := saved()
	_, err = svr.SetScreenSchedule(ctx, &pb.ScreenSchedule{OnTimes: []string{"30 7 * *"}})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())
	require.Contains(t, twerr.Msg(), `"30 7 * *"`)
	require.Len(t, s.cron.Entries(), 3, "a bad schedule leaves the jobs alone")
	require.Equal(t, before, saved(), "and the file")

	got, err := svr.GetSettings(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, int32(35), got.Brightness)
	require.Equal(t, []string{"30 7 * * *"}, got.ScreenSchedule.OnTimes)
	require.Equal(t, []string{"0 23 * * 0-4", "@midnight"}, got.ScreenSchedule.OffTimes)
	require.Equal(t, path, got.ConfigFile)
}
