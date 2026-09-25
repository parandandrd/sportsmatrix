package sportsmatrix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
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
	"github.com/parandandrd/sportsmatrix/internal/board/clock"
	imageboard "github.com/parandandrd/sportsmatrix/internal/board/image"
	racingboard "github.com/parandandrd/sportsmatrix/internal/board/racing"
	sportboard "github.com/parandandrd/sportsmatrix/internal/board/sport"
	statboard "github.com/parandandrd/sportsmatrix/internal/board/stat"
	sysboard "github.com/parandandrd/sportsmatrix/internal/board/sys"
	textboard "github.com/parandandrd/sportsmatrix/internal/board/text"
	"github.com/parandandrd/sportsmatrix/internal/conffile"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
	"github.com/parandandrd/sportsmatrix/internal/logo"
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
	current := mk("Current Conditions", "/weather/board.v1.BasicBoard/")
	forecast := mk("Forecast", "/forecast/weather/board.v1.BasicBoard/")
	stray := mk("Stray", "")

	s := &SportsMatrix{}
	s.SetBoardSections(map[board.Board]string{
		nhl: "nhlConfig", nhlStats: "nhlConfig", nhlHeadlines: "nhlConfig",
		pga: "pga", clockBoard: "clockConfig",
		current: "weatherConfig", forecast: "weatherConfig",
	}, nil)

	require.Equal(t, []string{"nhlConfig"}, s.boardKey(nhl))
	require.Equal(t, []string{"nhlConfig", "stats"}, s.boardKey(nhlStats))
	require.Equal(t, []string{"nhlConfig", "headlines"}, s.boardKey(nhlHeadlines))
	// PGA's stats board is its whole section, not a part of a league's
	require.Equal(t, []string{"pga"}, s.boardKey(pga))
	require.Equal(t, []string{"clockConfig"}, s.boardKey(clockBoard))
	require.Equal(t, []string{"weatherConfig"}, s.boardKey(current))
	require.Equal(t, []string{"weatherConfig", "forecast"}, s.boardKey(forecast))
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
		"board.v1.BasicBoard":      {clock.Config{}, sysboard.Config{}, textboard.Config{}, statboard.Config{}},
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

	cfg := &Config{ServeWebUI: false, HTTPListenPort: 18204}
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

func TestFormatDelay(t *testing.T) {
	t.Parallel()

	for d, want := range map[time.Duration]string{
		10 * time.Second:        "10s",
		90 * time.Second:        "1m30s",
		2 * time.Minute:         "2m",
		1500 * time.Millisecond: "1.5s",
	} {
		require.Equal(t, want, formatDelay(d))
		parsed, err := time.ParseDuration(want)
		require.NoError(t, err)
		require.Equal(t, d, parsed, "what it writes has to read back")
	}
}

// fakeTeam and fakeLeague are the least a sport board needs to be built and
// asked about its teams.
type fakeTeam struct{ abbrev, name string }

func (t fakeTeam) GetID() string           { return t.abbrev }
func (t fakeTeam) GetName() string         { return t.name }
func (t fakeTeam) GetAbbreviation() string { return t.abbrev }
func (t fakeTeam) GetDisplayName() string  { return t.name }
func (t fakeTeam) Score() int              { return 0 }
func (t fakeTeam) ConferenceName() string  { return "" }

type fakeLeague struct{}

func (fakeLeague) GetTeams(context.Context) ([]sportboard.Team, error) {
	return []sportboard.Team{
		fakeTeam{"NYR", "New York Rangers"},
		fakeTeam{"NYI", "New York Islanders"},
		fakeTeam{"BOS", "Boston Bruins"},
		// as ESPN's two team endpoints both return it
		fakeTeam{"NYI", "New York Islanders"},
	}, nil
}
func (fakeLeague) TeamFromID(context.Context, string) (sportboard.Team, error) { return nil, nil }
func (fakeLeague) GetScheduledGames(context.Context, []time.Time) ([]sportboard.Game, error) {
	return nil, nil
}
func (fakeLeague) DateStr(d time.Time) string { return d.Format("20060102") }
func (fakeLeague) League() string             { return "NHL" }
func (fakeLeague) HTTPPathPrefix() string     { return "nhl" }
func (fakeLeague) GetLogo(context.Context, string, *logo.Config, image.Rectangle) (*logo.Logo, error) {
	return nil, nil
}
func (fakeLeague) GetWatchTeams(teams []string, _ string) []string            { return teams }
func (fakeLeague) TeamRecord(context.Context, sportboard.Team, string) string { return "" }
func (fakeLeague) TeamRank(context.Context, sportboard.Team, string) string   { return "" }
func (fakeLeague) CacheClear(context.Context)                                 {}
func (fakeLeague) HomeSideSwap() bool                                         { return false }

func TestBoardSettings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))

	sportCfg := &sportboard.Config{BoardDelay: "10s", WatchTeams: []string{"ALL"}}
	sportCfg.SetDefaults()
	nhl, err := sportboard.New(ctx, fakeLeague{}, image.Rect(0, 0, 64, 32), nil, logger, sportCfg)
	require.NoError(t, err)

	clockCfg := &clock.Config{}
	clockCfg.SetDefaults()
	clockBoard, err := clock.New(clockCfg, logger)
	require.NoError(t, err)

	dat, err := os.ReadFile("../../sportsmatrix.conf.example")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "sportsmatrix.conf")
	require.NoError(t, os.WriteFile(path, dat, 0o644))
	file := conffile.New(path)
	sections, err := file.Sections()
	require.NoError(t, err)

	s := &SportsMatrix{log: logger, boards: []board.Board{nhl, clockBoard}}
	s.SetBoardSections(map[board.Board]string{nhl: "nhlConfig", clockBoard: "clockConfig"}, sections)
	s.SetConfigFile(file)
	svr := &Server{sm: s}

	saved := func(keys ...string) any {
		t.Helper()
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var cur any
		require.NoError(t, yamlv2.Unmarshal(raw, &cur))
		for _, k := range keys {
			cur = cur.(map[any]any)[k]
		}
		return cur
	}

	got, err := svr.GetBoardSettings(ctx, &pb.BoardSettingsReq{Name: "nhl"})
	require.NoError(t, err)
	require.True(t, got.HasBoardDelay)
	require.Equal(t, "10s", got.BoardDelay)
	require.Equal(t, "10s", got.MinBoardDelay)
	require.True(t, got.HasTeams)
	require.Equal(t, []string{"ALL"}, got.WatchTeams)
	require.Empty(t, got.FavoriteTeams)
	var names []string
	for _, team := range got.Teams {
		names = append(names, team.Abbreviation)
	}
	require.Equal(t, []string{"BOS", "NYI", "NYR"}, names, "by name, and once each")

	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "NHL", BoardDelay: "30s"})
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, nhl.BoardDelay())
	require.Equal(t, "30s", saved("nhlConfig", "boardDelay"))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(raw), "  boardDelay: \"30s\"\n", "quoted, as the file had it")

	var twerr twirp.Error
	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "NHL", BoardDelay: "5s"})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code(), "a sport board won't go under 10s")
	require.Equal(t, 30*time.Second, nhl.BoardDelay())

	// the clock has no floor of its own
	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "Clock", BoardDelay: "5s"})
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, clockBoard.BoardDelay())
	require.Equal(t, "5s", saved("clockConfig", "boardDelay"))

	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{
		Name:          "NHL",
		HasTeams:      true,
		WatchTeams:    []string{"nyi", " NYR ", "NYI"},
		FavoriteTeams: []string{"nyi"},
	})
	require.NoError(t, err)
	watch, favorite := nhl.Teams()
	require.Equal(t, []string{"NYI", "NYR"}, watch)
	require.Equal(t, []string{"NYI"}, favorite)
	require.Equal(t, []any{"NYI", "NYR"}, saved("nhlConfig", "watchTeams"))
	require.Equal(t, []any{"NYI"}, saved("nhlConfig", "favoriteTeams"))

	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "Clock", HasTeams: true})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())

	_, err = svr.GetBoardSettings(ctx, &pb.BoardSettingsReq{Name: "Stocks"})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.NotFound, twerr.Code())
}

// placeBoard is a board with a location, as the weather boards have.
type placeBoard struct {
	*pathBoard
	location string
}

func (b *placeBoard) Location(context.Context) (string, string) {
	if b.location == "" {
		return "", ""
	}
	return b.location, "Chicago, IL"
}

func (b *placeBoard) SetLocation(_ context.Context, loc string) (string, error) {
	if strings.Contains(loc, "London") {
		return "", errors.New("the Weather Service only covers the US")
	}
	b.location = strings.Join(strings.Fields(strings.ReplaceAll(loc, ",", " ")), ", ")
	return b.location, nil
}

func TestLocationSettings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))
	mk := func(name, path string) *pathBoard {
		return &pathBoard{TestBoard: &TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()}, name: name, path: path}
	}
	current := &placeBoard{pathBoard: mk("Current Conditions", "/weather/board.v1.BasicBoard/")}
	forecast := mk("Forecast", "/forecast/weather/board.v1.BasicBoard/")

	dat, err := os.ReadFile("../../sportsmatrix.conf.example")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "sportsmatrix.conf")
	require.NoError(t, os.WriteFile(path, dat, 0o644))
	file := conffile.New(path)
	sections, err := file.Sections()
	require.NoError(t, err)

	s := &SportsMatrix{log: logger, boards: []board.Board{current, forecast}}
	s.SetBoardSections(map[board.Board]string{current: "weatherConfig", forecast: "weatherConfig"}, sections)
	s.SetConfigFile(file)
	svr := &Server{sm: s}

	got, err := svr.GetBoardSettings(ctx, &pb.BoardSettingsReq{Name: "Current Conditions"})
	require.NoError(t, err)
	require.True(t, got.HasLocation)
	require.Empty(t, got.Location)

	got, err = svr.GetBoardSettings(ctx, &pb.BoardSettingsReq{Name: "Forecast"})
	require.NoError(t, err)
	require.False(t, got.HasLocation, "the location is set on the current conditions board")

	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "Current Conditions", HasLocation: true, Location: "41.8858,-87.6181"})
	require.NoError(t, err)

	got, err = svr.GetBoardSettings(ctx, &pb.BoardSettingsReq{Name: "Current Conditions"})
	require.NoError(t, err)
	require.Equal(t, "41.8858, -87.6181", got.Location)
	require.Equal(t, "Chicago, IL", got.LocationPlace)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var cfg map[string]any
	require.NoError(t, yamlv2.Unmarshal(raw, &cfg))
	weather, ok := cfg["weatherConfig"].(map[any]any)
	require.True(t, ok, "the weather section is in the file")
	require.Equal(t, "41.8858, -87.6181", weather["location"])

	var twerr twirp.Error
	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "Current Conditions", HasLocation: true, Location: "London"})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())
	require.Equal(t, "the Weather Service only covers the US", twerr.Msg(), "the board's own words")
	require.Equal(t, "41.8858, -87.6181", current.location)

	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "Current Conditions", HasLocation: true, Location: " "})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())

	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "Forecast", HasLocation: true, Location: "41, -87"})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())

	// the forecast board's switch goes in its own block
	require.NoError(t, s.setEnabled([]board.Board{forecast}, true))
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, yamlv2.Unmarshal(raw, &cfg))
	weather = cfg["weatherConfig"].(map[any]any)
	require.Equal(t, true, weather["forecast"].(map[any]any)["enabled"])
	require.Equal(t, false, weather["enabled"], "the current conditions board's switch is untouched")
}

// showBoard follows shows, as the TV board does.
type showBoard struct {
	*pathBoard
	shows []board.Show
}

var knownShows = map[int]board.Show{
	84:    {ID: 84, Name: "Family Guy", Network: "FOX", Premiered: "1999"},
	82759: {ID: 82759, Name: "9-1-1: Nashville", Network: "ABC", Premiered: "2025"},
}

func (b *showBoard) Shows(context.Context) []board.Show { return b.shows }

func (b *showBoard) SetShows(_ context.Context, shows []board.Show) ([]string, error) {
	var next []board.Show
	var saved []string
	for _, s := range shows {
		known, ok := knownShows[s.ID]
		if !ok {
			return nil, fmt.Errorf("no such show on TVmaze with the ID %d", s.ID)
		}
		next = append(next, known)
		saved = append(saved, fmt.Sprintf("%s (tvmaze %d)", known.Name, known.ID))
	}
	b.shows = next
	return saved, nil
}

func (b *showBoard) SearchShows(_ context.Context, query string) ([]board.Show, error) {
	if query == "" {
		return nil, errors.New("type a show's name to search for it")
	}
	return []board.Show{knownShows[82759], knownShows[84]}, nil
}

func TestShowSettings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))
	tv := &showBoard{pathBoard: &pathBoard{
		TestBoard: &TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()},
		name:      "TV Shows",
		path:      "/tv/board.v1.BasicBoard/",
	}}
	clockCfg := &clock.Config{}
	clockCfg.SetDefaults()
	clockBoard, err := clock.New(clockCfg, logger)
	require.NoError(t, err)

	dat, err := os.ReadFile("../../sportsmatrix.conf.example")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "sportsmatrix.conf")
	require.NoError(t, os.WriteFile(path, dat, 0o644))
	file := conffile.New(path)
	sections, err := file.Sections()
	require.NoError(t, err)

	s := &SportsMatrix{log: logger, boards: []board.Board{tv, clockBoard}}
	s.SetBoardSections(map[board.Board]string{tv: "tvConfig", clockBoard: "clockConfig"}, sections)
	s.SetConfigFile(file)
	svr := &Server{sm: s}

	saved := func() any {
		t.Helper()
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var cfg map[string]any
		require.NoError(t, yamlv2.Unmarshal(raw, &cfg))
		return cfg["tvConfig"].(map[any]any)["shows"]
	}

	got, err := svr.GetBoardSettings(ctx, &pb.BoardSettingsReq{Name: "TV Shows"})
	require.NoError(t, err)
	require.True(t, got.HasShows)
	require.Empty(t, got.Shows)

	found, err := svr.SearchShows(ctx, &pb.SearchShowsReq{Name: "tv shows", Query: "nashville"})
	require.NoError(t, err)
	require.Equal(t, "9-1-1: Nashville", found.Shows[0].Name)
	require.Equal(t, "ABC", found.Shows[0].Network)
	require.Equal(t, "2025", found.Shows[0].Premiered)

	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "TV Shows", HasShows: true, Shows: []*pb.Show{{Id: 84}, {Id: 82759}}})
	require.NoError(t, err)
	require.Equal(t, []any{"Family Guy (tvmaze 84)", "9-1-1: Nashville (tvmaze 82759)"}, saved())

	got, err = svr.GetBoardSettings(ctx, &pb.BoardSettingsReq{Name: "TV Shows"})
	require.NoError(t, err)
	require.Len(t, got.Shows, 2)
	require.Equal(t, "FOX", got.Shows[0].Network)

	var twerr twirp.Error
	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "TV Shows", HasShows: true, Shows: []*pb.Show{{Id: 5}}})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())
	require.Equal(t, "no such show on TVmaze with the ID 5", twerr.Msg())
	require.Len(t, saved(), 2, "a refused list leaves the file alone")

	_, err = svr.SearchShows(ctx, &pb.SearchShowsReq{Name: "TV Shows"})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())

	_, err = svr.SearchShows(ctx, &pb.SearchShowsReq{Name: "Clock", Query: "x"})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())

	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "Clock", HasShows: true})
	require.ErrorAs(t, err, &twerr)
	require.Equal(t, twirp.InvalidArgument, twerr.Code())

	// an empty list is saved as one
	_, err = svr.SetBoardSettings(ctx, &pb.BoardSettings{Name: "TV Shows", HasShows: true})
	require.NoError(t, err)
	require.Equal(t, []any{}, saved())
}
