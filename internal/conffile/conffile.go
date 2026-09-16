// Package conffile edits the YAML config file in place.
//
// The config file is written by hand as well as by the web UI, so an edit
// rewrites only the text of the values it changes: comments, blank lines,
// quoting and the order of everything else are left as they were. Decoding the
// file and encoding it again would not do that -- yaml.v3 keeps comments but
// drops blank lines -- and the file is read with YAML 1.1 rules besides, so
// what gets written has to mean the same thing to that reader.
package conffile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// File is a config file on disk.
type File struct {
	path string
	mu   sync.Mutex
}

// Edit sets the value at Path, a list of mapping keys from the top of the
// file, e.g. {"nhlConfig", "headlines", "enabled"}. Value is a bool, int,
// string or []string.
type Edit struct {
	Path  []string
	Value any
}

// New returns the config file at path.
func New(path string) *File {
	return &File{path: path}
}

// Path is where the file is.
func (f *File) Path() string {
	return f.path
}

// Apply makes edits in a single write. Keys match without regard to case, the
// way the config loader matches them, and keys the file doesn't have are added
// at the end of the section they belong in. An edit that would leave a value
// as it already is changes nothing, and if nothing changes nothing is written.
//
// Before anything is written, the new text is decoded the way the config is
// loaded and must equal the old config with exactly these edits made.
func (f *File) Apply(edits ...Edit) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path, old, err := f.read()
	if err != nil {
		return false, err
	}

	d := parse(old)
	for _, e := range edits {
		if len(e.Path) == 0 {
			return false, errors.New("an edit needs a key")
		}
		value, err := normalize(e.Value)
		if err != nil {
			return false, fmt.Errorf("%s: %w", strings.Join(e.Path, "."), err)
		}
		if has, err := d.hasValue(e.Path, value); err == nil && has {
			continue
		}
		if err := d.set(e.Path, e.Value); err != nil {
			return false, fmt.Errorf("%s: %w", strings.Join(e.Path, "."), err)
		}
	}

	out := d.bytes()
	if bytes.Equal(out, old) {
		return false, nil
	}

	if err := verifyEdits(old, out, edits); err != nil {
		return false, err
	}

	return true, writeFile(path, out)
}

// Sections lists the file's top-level keys in the order it has them.
func (f *File) Sections() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, dat, err := f.read()
	if err != nil {
		return nil, err
	}

	return parse(dat).topKeys()
}

// Order rearranges the top-level sections named in keys so they come in that
// order, moving each with the comments written above it. The sections trade
// the places they already occupy: everything not named stays where it is, and
// a key the file doesn't have is ignored.
func (f *File) Order(keys []string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path, old, err := f.read()
	if err != nil {
		return false, err
	}

	d := parse(old)
	if err := d.order(keys); err != nil {
		return false, err
	}

	out := d.bytes()
	if bytes.Equal(out, old) {
		return false, nil
	}

	if err := verifyOrder(old, out, keys); err != nil {
		return false, err
	}

	return true, writeFile(path, out)
}

func (f *File) read() (string, []byte, error) {
	// write through a symlink rather than over it
	path, err := filepath.EvalSymlinks(f.path)
	if err != nil {
		return "", nil, err
	}

	dat, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}

	return path, dat, nil
}

// writeFile replaces path with data all at once, so a crash or a full disk
// leaves the old file rather than half of the new one. The config is read by a
// service running as root, so the new file never keeps group or world write
// permission, whatever the old one had.
func writeFile(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm() &^ 0o022
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}
