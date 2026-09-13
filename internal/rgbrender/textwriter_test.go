package rgbrender

import (
	"bytes"
	"image"
	"image/color"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func testWriter(t testing.TB) (*TextWriter, *image.RGBA) {
	t.Helper()

	w, err := DefaultTextWriter()
	require.NoError(t, err)

	return w, image.NewRGBA(image.Rect(0, 0, 128, 32))
}

// GetFont hands out a shared parsed font rather than re-parsing the TTF on
// every call. A *truetype.Font is read-only once parsed, so sharing is safe.
func TestGetFontReturnsCachedFont(t *testing.T) {
	t.Parallel()

	a, err := GetFont(defaultFont)
	require.NoError(t, err)
	b, err := GetFont(defaultFont)
	require.NoError(t, err)

	require.Same(t, a, b)

	_, err = GetFont("does-not-exist.ttf")
	require.Error(t, err)
}

// The cached font.Face has to be rebuilt when FontSize is changed out from
// under the writer, which callers do routinely to fit a canvas.
func TestWriterHonorsFontSizeChange(t *testing.T) {
	t.Parallel()

	w, canvas := testWriter(t)

	w.FontSize = 8
	small, err := w.MeasureStrings(canvas, []string{"MMMM"})
	require.NoError(t, err)

	w.FontSize = 16
	large, err := w.MeasureStrings(canvas, []string{"MMMM"})
	require.NoError(t, err)

	require.Greater(t, large[0], small[0], "larger font size should measure wider")

	w.FontSize = 8
	backToSmall, err := w.MeasureStrings(canvas, []string{"MMMM"})
	require.NoError(t, err)
	require.Equal(t, small[0], backToSmall[0])
}

// MaxChars must still report the largest run of "M" that fits.
func TestMaxChars(t *testing.T) {
	t.Parallel()

	w, canvas := testWriter(t)

	for _, pixWidth := range []int{0, 1, 10, 32, 64, 128} {
		maxChars, err := w.MaxChars(canvas, pixWidth)
		require.NoError(t, err)

		if maxChars > 0 {
			fits, err := w.MeasureStrings(canvas, []string{string(bytes.Repeat([]byte("M"), maxChars))})
			require.NoError(t, err)
			require.LessOrEqual(t, fits[0], pixWidth, "reported max of %d does not fit %d px", maxChars, pixWidth)
		}

		tooMany, err := w.MeasureStrings(canvas, []string{string(bytes.Repeat([]byte("M"), maxChars+1))})
		require.NoError(t, err)
		require.Greater(t, tooMany[0], pixWidth, "one more than max should not fit %d px", pixWidth)
	}
}

// Color-coded text is positioned by measuring the prefix drawn so far. Writing
// the same characters in one color must land in the same pixels as a plain
// aligned write of the joined string.
func TestWriteAlignedColorCodesMatchesPlainWrite(t *testing.T) {
	t.Parallel()

	plainWriter, plain := testWriter(t)
	codeWriter, coded := testWriter(t)

	require.NoError(t, plainWriter.WriteAligned(CenterCenter, plain, plain.Bounds(), []string{"NYR 3-2"}, color.White))

	chars := &ColorChar{
		Lines: []*ColorCharLine{{}},
	}
	for _, r := range "NYR 3-2" {
		chars.Lines[0].Chars = append(chars.Lines[0].Chars, string(r))
		chars.Lines[0].Clrs = append(chars.Lines[0].Clrs, color.White)
	}
	require.NoError(t, codeWriter.WriteAlignedColorCodes(CenterCenter, coded, coded.Bounds(), chars))

	require.Equal(t, plain.Pix, coded.Pix)
}

// A single TextWriter is shared across the goroutines a LayerDrawer spawns, so
// its cached face must not be raced. Meaningful under -race.
func TestWriterConcurrentUse(t *testing.T) {
	t.Parallel()

	w, _ := testWriter(t)

	wg := sync.WaitGroup{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			canvas := image.NewRGBA(image.Rect(0, 0, 128, 32))
			for j := 0; j < 20; j++ {
				_ = w.WriteAligned(CenterCenter, canvas, canvas.Bounds(), []string{"NYR 3"}, color.White)
				_, _ = w.MeasureStrings(canvas, []string{"Blackhawks"})
				_, _ = w.MaxChars(canvas, 64)
			}
		}()
	}
	wg.Wait()
}

func BenchmarkWriteAligned(b *testing.B) {
	w, canvas := testWriter(b)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := w.WriteAligned(CenterCenter, canvas, canvas.Bounds(), []string{"NYR 3", "BOS 2"}, color.White); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteAlignedColorCodes(b *testing.B) {
	w, canvas := testWriter(b)
	chars := &ColorChar{Lines: []*ColorCharLine{{}}}
	for _, r := range "NYR 3-2" {
		chars.Lines[0].Chars = append(chars.Lines[0].Chars, string(r))
		chars.Lines[0].Clrs = append(chars.Lines[0].Clrs, color.White)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := w.WriteAlignedColorCodes(CenterCenter, canvas, canvas.Bounds(), chars); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMeasureStrings(b *testing.B) {
	w, canvas := testWriter(b)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := w.MeasureStrings(canvas, []string{"Chicago Blackhawks"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBreakText(b *testing.B) {
	w, canvas := testWriter(b)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := w.BreakText(canvas, 120, "Rangers beat Bruins in overtime thriller at the Garden"); err != nil {
			b.Fatal(err)
		}
	}
}
