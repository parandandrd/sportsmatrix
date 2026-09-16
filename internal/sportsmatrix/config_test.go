package sportsmatrix

import (
	"testing"

	"github.com/stretchr/testify/require"

	rgb "github.com/parandandrd/sportsmatrix/internal/rgbmatrix-rpi"
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

// Defaults turned a brightness of 100 into 60, to tone down the library's
// default, and so overrode a config that asked for 100 on purpose. Setting 100
// from the web UI would have come back as 60 after a restart.
func TestDefaultsKeepAChosenBrightness(t *testing.T) {
	t.Parallel()

	chosen := &Config{HardwareConfig: &rgb.HardwareConfig{Brightness: 100}}
	chosen.Defaults()
	require.Equal(t, 100, chosen.HardwareConfig.Brightness)

	unset := &Config{HardwareConfig: &rgb.HardwareConfig{}}
	unset.Defaults()
	require.Equal(t, 60, unset.HardwareConfig.Brightness)

	none := &Config{}
	none.Defaults()
	require.Equal(t, 60, none.HardwareConfig.Brightness)
}
