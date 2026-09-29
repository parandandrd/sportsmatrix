package util

import (
	"os"
	"path/filepath"
	"testing"
)

// nolint: paralleltest // t.Setenv can't be used in a parallel test
func TestCacheDir(t *testing.T) {
	t.Setenv("CACHE_DIRECTORY", "/var/cache/sportsmatrix")
	if got := cacheDir(); got != "/var/cache/sportsmatrix" {
		t.Errorf("under systemd: got %s", got)
	}

	t.Setenv("CACHE_DIRECTORY", "/var/cache/sportsmatrix:/var/cache/other")
	if got := cacheDir(); got != "/var/cache/sportsmatrix" {
		t.Errorf("with two directories: got %s", got)
	}

	t.Setenv("CACHE_DIRECTORY", "")
	if got, want := cacheDir(), filepath.Join(os.TempDir(), "sportsmatrix"); got != want {
		t.Errorf("outside systemd: got %s, want %s", got, want)
	}
}
