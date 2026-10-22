package sportboard

import (
	"image"
	"testing"
)

func TestLogoPos(t *testing.T) {
	t.Parallel()

	panel := image.Rect(0, 0, 64, 32)
	for _, tc := range []struct {
		name         string
		area, w, h   int
		right        bool
		wantX, wantY int
	}{
		// The 28x28 a square logo gets with the gradient on: flush with the
		// outer edge, a pixel under the score, two rows clear top and bottom.
		{"square left", 28, 28, 28, false, 0, 2},
		{"square right", 28, 28, 28, true, 36, 2},
		{"wide left", 28, 28, 14, false, 0, 9},
		{"narrow left", 28, 20, 28, false, 4, 2},
		{"narrow right", 28, 20, 28, true, 40, 2},
		{"odd gap mirrors", 28, 27, 27, false, 0, 2},
		{"odd gap mirrors right", 28, 27, 27, true, 37, 2},
		// Zoomed past its area: off the outer edge, never under the score.
		{"too wide left", 28, 30, 30, false, -2, 1},
		{"too wide right", 28, 30, 30, true, 36, 1},
	} {
		x, y := logoPos(panel, tc.area, tc.w, tc.h, tc.right)
		if x != tc.wantX || y != tc.wantY {
			t.Errorf("%s: at %d,%d, want %d,%d", tc.name, x, y, tc.wantX, tc.wantY)
		}
	}
}
