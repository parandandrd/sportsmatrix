package rgbmatrix

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestNewRGBLedMatrixRefused covers the library refusing to make a matrix.
// It returns NULL then, and the offscreen canvas used to be made from that
// NULL before it was checked, which crashed the process in C instead of
// returning an error. Options the library rejects make it return NULL before
// it touches any GPIO, so this is safe to run anywhere.
func TestNewRGBLedMatrixRefused(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig
	cfg.Cols = 64
	cfg.HardwareMapping = "regular"
	cfg.PWMBits = 99

	rt := DefaultRuntimeOptions

	m, err := NewRGBLedMatrix(&cfg, &rt, zap.NewNop())
	require.Error(t, err)
	require.Nil(t, m)
}
