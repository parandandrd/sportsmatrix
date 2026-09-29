package logo

import (
	"image"
	"testing"
)

func TestShrinkSource(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		width, height int
		wantW, wantH  int
	}{
		// ESPN's biggest logos, which were cached at 67MB each
		{"huge square", 4096, 4096, 256, 256},
		{"wide", 1000, 500, 256, 128},
		{"tall", 300, 600, 128, 256},
		{"one side too big", 512, 100, 256, 50},
	} {
		src := image.NewNRGBA(image.Rect(0, 0, tc.width, tc.height))
		got := ShrinkSource(src).Bounds()
		if got.Dx() != tc.wantW || got.Dy() != tc.wantH {
			t.Errorf("%s: %dx%d shrank to %dx%d, want %dx%d", tc.name, tc.width, tc.height, got.Dx(), got.Dy(), tc.wantW, tc.wantH)
		}
	}
}

func TestShrinkSourceLeavesSmallLogos(t *testing.T) {
	t.Parallel()

	for _, size := range []image.Rectangle{image.Rect(0, 0, 256, 256), image.Rect(0, 0, 64, 32)} {
		src := image.NewNRGBA(size)
		if got := ShrinkSource(src); got != image.Image(src) {
			t.Errorf("%v was copied, not returned as it was", size)
		}
	}
}
