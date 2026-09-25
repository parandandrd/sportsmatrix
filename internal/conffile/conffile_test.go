package conffile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// example copies the shipped config somewhere writable. It is what a fresh
// install runs, and every edit here has to leave the rest of it byte for byte.
func example(t *testing.T) (*File, string, []byte) {
	t.Helper()

	dat, err := os.ReadFile("../../sportsmatrix.conf.example")
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "sportsmatrix.conf")
	require.NoError(t, os.WriteFile(path, dat, 0o644))

	return New(path), path, dat
}

func write(t *testing.T, text string) (*File, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sportsmatrix.conf")
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
	return New(path), path
}

func read(t *testing.T, path string) string {
	t.Helper()
	dat, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(dat)
}

// changed lists the lines that differ, for files with the same line count.
func changed(t *testing.T, before []byte, after string) []string {
	t.Helper()
	a, b := strings.Split(string(before), "\n"), strings.Split(after, "\n")
	require.Len(t, b, len(a), "line count changed")
	var out []string
	for i := range a {
		if a[i] != b[i] {
			out = append(out, b[i])
		}
	}
	return out
}

func TestApplyChangesOnlyThatValue(t *testing.T) {
	t.Parallel()
	f, path, before := example(t)

	ok, err := f.Apply(Edit{Path: []string{"nhlConfig", "enabled"}, Value: false})
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []string{"  enabled: false"}, changed(t, before, read(t, path)))

	// the NHL section's, not any other league's
	after := read(t, path)
	nhl := after[strings.Index(after, "nhlConfig:"):]
	require.True(t, strings.HasPrefix(nhl, "nhlConfig:\n  enabled: false\n"))
}

func TestApplyNestedKeepsQuotes(t *testing.T) {
	t.Parallel()
	f, path, before := example(t)

	_, err := f.Apply(
		Edit{Path: []string{"nhlConfig", "headlines", "enabled"}, Value: true},
		Edit{Path: []string{"clockConfig", "boardDelay"}, Value: "15s"},
		Edit{Path: []string{"sportsMatrixConfig", "hardwareConfig", "brightness"}, Value: 40},
		Edit{Path: []string{"sportsMatrixConfig", "screenOnTimes"}, Value: []string{"30 7 * * *"}},
	)
	require.NoError(t, err)

	require.ElementsMatch(t, []string{
		"    enabled: true",
		`  boardDelay: "15s"`,
		"    brightness: 40",
		`  - "30 7 * * *"`,
	}, changed(t, before, read(t, path)))

	after := read(t, path)
	nhl := after[strings.Index(after, "nhlConfig:"):]
	require.Contains(t, nhl[:strings.Index(nhl, "stats:")], "  headlines:\n    enabled: true\n")
}

func TestApplyLists(t *testing.T) {
	t.Parallel()
	f, path, _ := example(t)

	_, err := f.Apply(Edit{Path: []string{"mlbConfig", "watchTeams"}, Value: []string{"ATL", "NYM"}})
	require.NoError(t, err)
	after := read(t, path)
	require.Contains(t, after, "  # - Divisions: NLE, NLC, NLW, ALE, ALC, ALW\n  watchTeams:\n  - ATL\n  - NYM\n\n")

	// a key the section only has commented out is added, and a value a YAML
	// 1.1 reader would take for a boolean is quoted
	_, err = f.Apply(Edit{Path: []string{"nflConfig", "favoriteTeams"}, Value: []string{"NO", "KC"}})
	require.NoError(t, err)
	after = read(t, path)
	nfl := after[strings.Index(after, "nflConfig:"):strings.Index(after, "## MLS Config")]
	require.Contains(t, nfl, "  enable24Hour: false\n  favoriteTeams:\n  - \"NO\"\n  - KC\n")
	require.Contains(t, nfl, "  #favoriteTeams:\n  #- ATL\n", "the commented-out example stays as it was")

	// and emptied
	_, err = f.Apply(Edit{Path: []string{"mlbConfig", "watchTeams"}, Value: []string{}})
	require.NoError(t, err)
	require.Contains(t, read(t, path), "  watchTeams: []\n\n")
}

func TestApplyAddsSections(t *testing.T) {
	t.Parallel()
	f, path, before := example(t)

	_, err := f.Apply(Edit{Path: []string{"uefaConfig", "enabled"}, Value: true})
	require.NoError(t, err)

	after := read(t, path)
	require.True(t, strings.HasPrefix(after, string(before)))
	require.True(t, strings.HasSuffix(after, "\n\nuefaConfig:\n  enabled: true\n"), after[len(after)-80:])

	keys, err := f.Sections()
	require.NoError(t, err)
	require.Equal(t, "uefaConfig", keys[len(keys)-1])
}

func TestApplyMatchesKeysLikeTheLoader(t *testing.T) {
	t.Parallel()
	f, path, before := example(t)

	_, err := f.Apply(Edit{Path: []string{"NHLCONFIG", "Enabled"}, Value: false})
	require.NoError(t, err)
	require.Equal(t, []string{"  enabled: false"}, changed(t, before, read(t, path)))
}

func TestApplyLeavesAnUnchangedFileAlone(t *testing.T) {
	t.Parallel()
	f, path, before := example(t)

	past := info(t, path).ModTime().Add(-1e9 * 3600)
	require.NoError(t, os.Chtimes(path, past, past))

	ok, err := f.Apply(
		Edit{Path: []string{"nhlConfig", "enabled"}, Value: true},
		Edit{Path: []string{"clockConfig", "boardDelay"}, Value: "10s"},
		Edit{Path: []string{"ncaamConfig", "watchTeams"}, Value: []string{"SEC", "ACC", "BIG10"}},
	)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, string(before), read(t, path))
	require.Equal(t, past.Unix(), info(t, path).ModTime().Unix())
}

func TestApplyAwkwardText(t *testing.T) {
	t.Parallel()

	// not "on", which YAML 1.1 reads as the key true
	f, path := write(t, "a:\n  shown: true # keep this\n  delay: '10s'\n  empty:\n  teams: ALL\n  flow: [x, \"y\"]\n  sub:\n")

	_, err := f.Apply(
		Edit{Path: []string{"a", "shown"}, Value: false},
		Edit{Path: []string{"a", "delay"}, Value: "it's"},
		Edit{Path: []string{"a", "empty"}, Value: "now"},
		Edit{Path: []string{"a", "teams"}, Value: []string{"NYI", "yes"}},
		Edit{Path: []string{"a", "flow"}, Value: []string{"x", "a, b"}},
		Edit{Path: []string{"a", "sub", "deep"}, Value: 3},
	)
	require.NoError(t, err)
	require.Equal(t, "a:\n  shown: false # keep this\n  delay: 'it''s'\n  empty: now\n  teams:\n  - NYI\n  - \"yes\"\n  flow: [x, \"a, b\"]\n  sub:\n    deep: 3\n", read(t, path))
}

func TestApplyRefusesWhatItCannotDoCleanly(t *testing.T) {
	t.Parallel()
	f, path, before := example(t)

	_, err := f.Apply(Edit{Path: []string{"clockConfig"}, Value: true})
	require.Error(t, err, "a section is not a value")
	_, err = f.Apply(Edit{Path: []string{"mlbConfig", "watchTeams", "x"}, Value: true})
	require.Error(t, err, "a list is not a section")
	_, err = f.Apply(Edit{Path: []string{"nhlConfig", "enabled"}, Value: 1.5})
	require.Error(t, err, "only the types the settings use")

	require.Equal(t, string(before), read(t, path))
}

func TestOrderMovesSectionsWithTheirHeadings(t *testing.T) {
	t.Parallel()
	f, path, before := example(t)

	ok, err := f.Order([]string{"nhlConfig", "serieaConfig", "clockConfig", "nosuchConfig"})
	require.NoError(t, err)
	require.True(t, ok)

	keys, err := f.Sections()
	require.NoError(t, err)
	// the three trade the places they had, first, second and third among
	// themselves; everything else is where it was
	require.Equal(t, []string{"sportsMatrixConfig", "nhlConfig", "sysConfig", "ncaafConfig", "serieaConfig"}, keys[:5])
	require.Equal(t, "clockConfig", keys[22])

	after := read(t, path)
	require.True(t, strings.HasPrefix(after, "---\n# This config file is in YAML format, which means indention matters.\n\n# Main matrix config\nsportsMatrixConfig:\n"))
	require.Contains(t, after, "## NHL config\nnhlConfig:\n  enabled: true\n")
	require.Contains(t, after, "# Clock Board\nclockConfig:\n")
	// the one heading in the file that is indented still goes with its section
	require.Contains(t, after, "  ## Italian Serie A Config\nserieaConfig:\n")
	// and the commented-out options at the end of a section go with it: the
	// clock's are now at the end of the file, ahead of La Liga
	require.Contains(t, after, "  #offTimes:\n  #- 00 02 * * *\n\n## Spanish La Liga Config\nlaligaConfig:")
	require.Contains(t, after, "  previousDays: 0\n\n## Sys board shows")
	require.Len(t, strings.Split(after, "\n"), len(strings.Split(string(before), "\n")))

	// putting them back restores the file exactly
	_, err = f.Order([]string{"clockConfig", "nhlConfig", "serieaConfig"})
	require.NoError(t, err)
	require.Equal(t, string(before), read(t, path))
}

func TestOrderAlreadyInOrderWritesNothing(t *testing.T) {
	t.Parallel()
	f, path, before := example(t)

	ok, err := f.Order([]string{"clockConfig", "nhlConfig", "xflConfig"})
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, string(before), read(t, path))
}

func TestOrderKeepsTheLastSectionApart(t *testing.T) {
	t.Parallel()

	f, path := write(t, "# head\n\none:\n  a: 1\n\n# two\ntwo:\n  b: 2")
	_, err := f.Order([]string{"two", "one"})
	require.NoError(t, err)
	require.Equal(t, "# head\n\n# two\ntwo:\n  b: 2\n\none:\n  a: 1", read(t, path))
}

func TestWritesDropGroupAndWorldWrite(t *testing.T) {
	t.Parallel()
	f, path, _ := example(t)

	// how the .deb has been installing it
	require.NoError(t, os.Chmod(path, 0o666))

	_, err := f.Apply(Edit{Path: []string{"nhlConfig", "enabled"}, Value: false})
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info(t, path).Mode().Perm())

	// no temp files left behind
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestWritesThroughASymlink(t *testing.T) {
	t.Parallel()
	_, target, _ := example(t)

	link := filepath.Join(t.TempDir(), "sportsmatrix.conf")
	require.NoError(t, os.Symlink(target, link))

	_, err := New(link).Apply(Edit{Path: []string{"nhlConfig", "enabled"}, Value: false})
	require.NoError(t, err)

	fi, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, fi.Mode()&os.ModeSymlink, "the link is still a link")
	require.Contains(t, read(t, target), "nhlConfig:\n  enabled: false\n")
}

func info(t *testing.T, path string) os.FileInfo {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	return fi
}
