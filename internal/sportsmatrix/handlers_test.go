package sportsmatrix

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/parandandrd/sportsmatrix/internal/enabler"
)

// newIdleMatrix is a matrix that has been set up but isn't serving.
func newIdleMatrix(t *testing.T) *SportsMatrix {
	t.Helper()

	logger := zaptest.NewLogger(t, zaptest.Level(zapcore.FatalLevel))

	cfg := &Config{ServeWebUI: false}
	cfg.Defaults()

	b := &TestBoard{log: logger, hasRendered: atomic.NewBool(false), enabler: enabler.New()}
	s, err := New(context.Background(), logger, cfg, nil, b)
	require.NoError(t, err)

	return s
}

func handler(t *testing.T, s *SportsMatrix, path string) http.HandlerFunc {
	t.Helper()

	for _, h := range s.httpHandlers() {
		if h.Path == path {
			return h.Handler
		}
	}
	t.Fatalf("no handler for %s", path)

	return nil
}

// TestJumpBadBody covers /api/jump with a body that isn't a jump request. It
// dereferenced a nil request and panicked.
//
// nolint: paralleltest
func TestJumpBadBody(t *testing.T) {
	jump := handler(t, newIdleMatrix(t), "/api/jump")

	for _, body := range []string{"", "null", "not json", "{}"} {
		rec := httptest.NewRecorder()
		jump(rec, httptest.NewRequest(http.MethodPost, "/api/jump", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, rec.Code, "body %q", body)
	}
}

// TestNextBoardBeforeAnyBoard covers skipping to the next board before the
// first one has started. The cancel func it called was still nil.
//
// nolint: paralleltest
func TestNextBoardBeforeAnyBoard(t *testing.T) {
	s := newIdleMatrix(t)

	rec := httptest.NewRecorder()
	handler(t, s, "/api/nextboard")(rec, httptest.NewRequest(http.MethodGet, "/api/nextboard", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	_, err := (&Server{sm: s}).NextBoard(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
}
