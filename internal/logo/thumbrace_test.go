package logo

import (
	"bytes"
	"context"
	"image"
	_ "image/png"
	"os"
	"testing"
)

// GetThumbnail saves the thumbnail to disk on a fire-and-forget goroutine and
// returns immediately. The next caller stats that file, finds it, and reads it
// while the write is still in flight.
//
// The text board makes this easy to hit: it never stores its *Logo back in its
// map, so every render builds a fresh one and every render re-enters this path.
//
// nolint: paralleltest
func TestThumbnailReadWhileStillBeingWritten(t *testing.T) {
	dir := t.TempDir()
	bounds := image.Rect(0, 0, 64, 32)

	dat, err := os.ReadFile("../espnboard/assets/league_logos/nwsl.png")
	if err != nil {
		t.Fatal(err)
	}
	get := func(context.Context) (image.Image, error) {
		img, _, err := image.Decode(bytes.NewReader(dat))
		return img, err
	}
	conf := &Config{Abbrev: "news", XSize: 64, YSize: 32, Pt: &Pt{Zoom: 1}}

	// First render: decodes the source and kicks off the async save.
	first := New("news_64x32", get, dir, bounds, conf)
	if _, err := first.GetThumbnail(context.Background(), bounds); err != nil {
		t.Fatalf("first render: %v", err)
	}

	// Immediately after, exactly as the next headline in the same loop does.
	fails := 0
	for i := 0; i < 200; i++ {
		next := New("news_64x32", get, dir, bounds, conf)
		if _, err := next.GetThumbnail(context.Background(), bounds); err != nil {
			fails++
			t.Logf("render %d failed: %v", i, err)
			break
		}
	}

	if fails > 0 {
		t.Errorf("%d render(s) read a half-written thumbnail", fails)
	}
}
