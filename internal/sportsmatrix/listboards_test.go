package sportsmatrix

import (
	"context"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/parandandrd/sportsmatrix/internal/board"
	textboard "github.com/parandandrd/sportsmatrix/internal/board/text"
	"github.com/parandandrd/sportsmatrix/internal/enabler"
)

// namedBoard is a TestBoard that reports a given name, so ListBoards has
// something distinguishable to return.
type namedBoard struct {
	*TestBoard
	name string
}

func (b *namedBoard) Name() string { return b.name }

// headlineAPI is the smallest thing textboard.New will accept. Headline boards
// are the case that used to break: every one of them answered "Texts".
type headlineAPI struct{ prefix string }

func (h *headlineAPI) GetLogo(ctx context.Context) (image.Image, error) { return nil, nil }
func (h *headlineAPI) GetText(ctx context.Context) ([]string, error)    { return nil, nil }
func (h *headlineAPI) HTTPPathPrefix() string                           { return h.prefix }

func newHeadlines(t *testing.T, logger *zap.Logger, league string) *textboard.TextBoard {
	t.Helper()

	cfg := &textboard.Config{StartEnabled: atomic.NewBool(false)}
	cfg.SetDefaults()

	b, err := textboard.New(&headlineAPI{prefix: league}, cfg, logger)
	require.NoError(t, err)

	return b
}

// ListBoards has to report the boards this instance was actually built with,
// and track their enabled state as it changes at runtime.
func TestListBoardsOverHTTP(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.ErrorLevel))
	cfg := &Config{ServeWebUI: false, HTTPListenPort: 8099, WebBoardWidth: 1}
	cfg.Defaults()

	canvas := board.NewBlankCanvas(1, 1, logger)
	canvas.Enable()

	mk := func(name string, on bool) *namedBoard {
		b := &namedBoard{
			TestBoard: &TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()},
			name:      name,
		}
		if on {
			b.enabler.Enable()
		} else {
			b.enabler.Disable()
		}
		return b
	}

	on, off := mk("nhl", true), mk("mlb", false)
	nhlHeadlines, mlbHeadlines := newHeadlines(t, logger, "nhl"), newHeadlines(t, logger, "mlb")

	s, err := New(ctx, logger, cfg, []board.Canvas{canvas}, on, off, nhlHeadlines, mlbHeadlines)
	require.NoError(t, err)
	defer s.Close()

	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = s.Serve(ctx)
	}()

	// Serve logs through the test's logger, so it has to be done before the
	// test is -- a goroutine still logging after the test returns is a race.
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

	post := func(method string, body string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"http://localhost:8099/matrix.v1.Sportsmatrix/"+method, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		out, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		return resp.StatusCode, out
	}

	list := func() map[string]bool {
		t.Helper()
		code, body := post("ListBoards", "{}")
		require.Equal(t, http.StatusOK, code)

		var got struct {
			Boards []struct {
				Name      string `json:"name"`
				Enabled   bool   `json:"enabled"`
				InBetween bool   `json:"in_between"`
			} `json:"boards"`
		}
		require.NoError(t, json.Unmarshal(body, &got), "body was: %s", body)

		out := map[string]bool{}
		for _, b := range got.Boards {
			_, dup := out[b.Name]
			require.False(t, dup, "board names have to be unique to be addressable: %q twice", b.Name)
			out[b.Name] = b.Enabled
		}
		return out
	}

	paths := func() map[string]string {
		t.Helper()
		code, body := post("ListBoards", "{}")
		require.Equal(t, http.StatusOK, code)

		var got struct {
			Boards []struct {
				Name    string `json:"name"`
				RPCPath string `json:"rpc_path"`
			} `json:"boards"`
		}
		require.NoError(t, json.Unmarshal(body, &got), "body was: %s", body)

		out := map[string]string{}
		for _, b := range got.Boards {
			out[b.Name] = b.RPCPath
		}
		return out
	}

	boards := list()
	require.Len(t, boards, 4, "should report exactly the boards it was built with")
	require.True(t, boards["nhl"], "nhl was enabled")
	require.False(t, boards["mlb"], "mlb was disabled")

	// two headline boards, two distinct names
	require.Contains(t, boards, "NHL Headlines")
	require.Contains(t, boards, "MLB Headlines")

	// rpc_path says where a board's own service lives, so a client can reach
	// its settings without a hardcoded table of leagues
	require.Equal(t, map[string]string{
		"nhl":           "",
		"mlb":           "",
		"NHL Headlines": "/headlines/nhl/board.v1.BasicBoard/",
		"MLB Headlines": "/headlines/mlb/board.v1.BasicBoard/",
	}, paths())

	// and each headline board can be addressed on its own
	code, body := post("SetBoardEnabled", `{"name":"NHL Headlines","enabled":true}`)
	require.Equal(t, http.StatusOK, code, "body was: %s", body)
	require.True(t, list()["NHL Headlines"])
	require.False(t, list()["MLB Headlines"], "the other headline board must be untouched")

	// state changes at runtime must be reflected
	off.enabler.Enable()
	require.True(t, list()["mlb"], "ListBoards should track a board enabled after startup")

	// SetBoardEnabled is the other half: change one board's state by the name
	// ListBoards just reported, without knowing what kind of board it is.
	code, body = post("SetBoardEnabled", `{"name":"mlb","enabled":false}`)
	require.Equal(t, http.StatusOK, code, "body was: %s", body)
	require.False(t, list()["mlb"], "SetBoardEnabled should have turned mlb off")
	require.True(t, list()["nhl"], "and left the other board alone")

	// Jump matches names case-insensitively, so this must too
	code, body = post("SetBoardEnabled", `{"name":"MLB","enabled":true}`)
	require.Equal(t, http.StatusOK, code, "body was: %s", body)
	require.True(t, list()["mlb"], "SetBoardEnabled should match the name case-insensitively")

	// a name this instance does not have is an error, not a silent no-op
	code, body = post("SetBoardEnabled", `{"name":"nfl","enabled":true}`)
	require.Equal(t, http.StatusNotFound, code, "body was: %s", body)
	require.Contains(t, string(body), "not_found")
	require.Len(t, list(), 4, "and must not have invented a board")
}
