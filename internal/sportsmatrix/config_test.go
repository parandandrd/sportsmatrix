package sportsmatrix

import (
	"testing"

	"github.com/stretchr/testify/require"

	rgb "github.com/robbydyer/sports/internal/rgbmatrix-rpi"
)

// Defaults used to point every Config at the package-level default structs and
// then write through the pointer, so two Configs shared one HardwareConfig and
// the defaults themselves were edited in the process.
func TestDefaultsDoNotAliasPackageDefaults(t *testing.T) {
	t.Parallel()

	rows, cols := rgb.DefaultConfig.Rows, rgb.DefaultConfig.Cols
	daemon := rgb.DefaultRuntimeOptions.Daemon

	a := &Config{}
	a.Defaults()
	b := &Config{}
	b.Defaults()

	require.NotSame(t, a.HardwareConfig, b.HardwareConfig, "each Config needs its own hardware config")
	require.NotSame(t, a.RuntimeOptions, b.RuntimeOptions, "each Config needs its own runtime options")

	a.HardwareConfig.Brightness = 99
	require.NotEqual(t, 99, b.HardwareConfig.Brightness, "one Config must not write into another")

	require.Equal(t, rows, rgb.DefaultConfig.Rows, "the package defaults must survive a Defaults() call")
	require.Equal(t, cols, rgb.DefaultConfig.Cols)
	require.Equal(t, daemon, rgb.DefaultRuntimeOptions.Daemon)
}
