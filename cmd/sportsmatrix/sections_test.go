package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/parandandrd/sportsmatrix/internal/config"
)

func TestConfigSections(t *testing.T) {
	t.Parallel()

	keys, err := configSections([]byte(`
# comments and nesting are not sections
clockConfig:
  enabled: true
nhlConfig:
  headlines:
    enabled: false
pga:
  enabled: false
`))
	require.NoError(t, err)
	require.Equal(t, []string{"clockConfig", "nhlConfig", "pga"}, keys)

	keys, err = configSections(nil)
	require.NoError(t, err)
	require.Empty(t, keys)
}

// The shipped example is what a fresh install runs, so its layout is the one
// most dashboards will show.
func TestConfigSectionsOfExample(t *testing.T) {
	t.Parallel()

	dat, err := os.ReadFile("../../sportsmatrix.conf.example")
	require.NoError(t, err)

	keys, err := configSections(dat)
	require.NoError(t, err)
	require.Equal(t, []string{"sportsMatrixConfig", "clockConfig", "sysConfig", "ncaafConfig", "nhlConfig"}, keys[:5])
	require.NotContains(t, keys, "uefaConfig", "the example has no UEFA section, though the board is still built")
}

func TestConfigKey(t *testing.T) {
	t.Parallel()

	c := &config.Config{}

	require.Equal(t, "nhlConfig", configKey(c, &c.NHLConfig))
	require.Equal(t, "pga", configKey(c, &c.PGA))

	// a field of some other Config is not one of this one's sections
	other := &config.Config{}
	require.Empty(t, configKey(c, &other.NHLConfig))
}
