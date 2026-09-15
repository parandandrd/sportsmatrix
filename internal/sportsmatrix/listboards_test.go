package sportsmatrix

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/robbydyer/sports/internal/board"
	"github.com/robbydyer/sports/internal/enabler"
)

// namedBoard is a TestBoard that reports a given name, so ListBoards has
// something distinguishable to return.
type namedBoard struct {
	*TestBoard
	name string
}

func (b *namedBoard) Name() string { return b.name }

// ListBoards has to report the boards this instance was actually built with,
// and track their enabled state as it changes at runtime.
func TestListBoardsOverHTTP(t *testing.T) {
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

	s, err := New(ctx, logger, cfg, []board.Canvas{canvas}, on, off)
	require.NoError(t, err)
	defer s.Close()

	go func() { _ = s.Serve(ctx) }()
	select {
	case <-s.isServing:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the matrix to serve")
	}

	list := func() map[string]bool {
		t.Helper()
		resp, err := http.Post("http://localhost:8099/matrix.v1.Sportsmatrix/ListBoards",
			"application/json", strings.NewReader("{}"))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

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
			out[b.Name] = b.Enabled
		}
		return out
	}

	boards := list()
	require.Len(t, boards, 2, "should report exactly the boards it was built with")
	require.True(t, boards["nhl"], "nhl was enabled")
	require.False(t, boards["mlb"], "mlb was disabled")

	// state changes at runtime must be reflected
	off.enabler.Enable()
	require.True(t, list()["mlb"], "ListBoards should track a board enabled after startup")
}
