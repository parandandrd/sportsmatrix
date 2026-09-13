package rgbrender

import (
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"path/filepath"
	"strings"
	"sync"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

//go:embed assets/fonts
var fontDir embed.FS

const (
	defaultFont = "04b24.ttf"
)

// BuiltinFonts is a list of fonts names this pkg provides
var BuiltinFonts = []string{
	"04b24.ttf",
	"BlockStockRegular-A71p.ttf",
	"04B_03__.ttf",
	"score.ttf",
}

// TextWriter ...
type TextWriter struct {
	font             *truetype.Font
	XStartCorrection int
	YStartCorrection int
	FontSize         float64
	LineSpace        float64

	// lock guards the cached drawing state below. A font.Face carries an
	// internal glyph cache that isn't safe for concurrent use, and a single
	// TextWriter is shared by the goroutines a LayerDrawer spawns per layer.
	lock     sync.Mutex
	face     font.Face
	faceSize float64
	uniform  *image.Uniform
	drawer   *font.Drawer
}

// ColorChar is used to define text for writing in different colors
type ColorChar struct {
	Lines  []*ColorCharLine
	BoxClr color.Color
}

// ColorCharLine is a line in a multicolored text
type ColorCharLine struct {
	Chars []string
	Clrs  []color.Color
}

// DefaultTextWriter ...
func DefaultTextWriter() (*TextWriter, error) {
	fnt, err := GetFont(defaultFont)
	if err != nil {
		return nil, err
	}

	t := NewTextWriter(fnt, 8)
	t.YStartCorrection = -2

	return t, nil
}

// NewTextWriter ...
func NewTextWriter(font *truetype.Font, fontSize float64) *TextWriter {
	return &TextWriter{
		font:      font,
		FontSize:  fontSize,
		LineSpace: 0.5,
	}
}

// fontCache holds the parsed builtin fonts. A *truetype.Font is read-only
// once parsed, so it's safe to hand the same one to every caller.
var fontCache sync.Map

// GetFont gets a builtin font by the given name
func GetFont(name string) (*truetype.Font, error) {
	if !strings.HasPrefix("assets/fonts", name) {
		name = filepath.Join("assets/fonts", name)
	}

	if cached, ok := fontCache.Load(name); ok {
		return cached.(*truetype.Font), nil
	}

	f, err := fontDir.ReadFile(name)
	if err != nil {
		return nil, err
	}

	fnt, err := freetype.ParseFont(f)
	if err != nil {
		return nil, err
	}

	actual, _ := fontCache.LoadOrStore(name, fnt)

	return actual.(*truetype.Font), nil
}

// getDrawer returns this writer's drawer, rebuilding the underlying font.Face
// only when FontSize has changed since the last call. Building a face is
// expensive and it carries a glyph cache worth hanging on to.
//
// The returned drawer is owned by the TextWriter and is only valid while
// t.lock is held, so callers must hold the lock for as long as they use it.
func (t *TextWriter) getDrawer(canvas draw.Image, clr color.Color) (*font.Drawer, error) {
	if t.font == nil {
		return nil, fmt.Errorf("font is not set")
	}

	if t.face == nil || t.faceSize != t.FontSize {
		if t.face != nil {
			_ = t.face.Close()
		}
		t.face = truetype.NewFace(t.font,
			&truetype.Options{
				Size:    t.FontSize,
				Hinting: font.HintingFull,
			},
		)
		t.faceSize = t.FontSize
	}

	if t.uniform == nil {
		t.uniform = image.NewUniform(clr)
	} else {
		t.uniform.C = clr
	}

	if t.drawer == nil {
		t.drawer = &font.Drawer{}
	}
	t.drawer.Dst = canvas
	t.drawer.Src = t.uniform
	t.drawer.Face = t.face
	t.drawer.Dot = fixed.Point26_6{}

	return t.drawer, nil
}

// Write ...
func (t *TextWriter) Write(canvas draw.Image, bounds image.Rectangle, str []string, clr color.Color) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	drawer, err := t.getDrawer(canvas, clr)
	if err != nil {
		return err
	}
	startX := bounds.Min.X + t.XStartCorrection

	// lineY represents how much space to add for the newline
	lineY := int(math.Floor(t.FontSize + t.LineSpace))

	y := int(math.Floor(t.FontSize)) + bounds.Min.Y + t.YStartCorrection
	drawer.Dot = fixed.P(startX, y)

	for _, s := range str {
		drawer.DrawString(s)
		y += lineY + t.YStartCorrection
		drawer.Dot = fixed.P(startX, y)
	}

	return nil
}

// WriteAligned writes text aligned within a given bounds
func (t *TextWriter) WriteAligned(align Align, canvas draw.Image, bounds image.Rectangle, str []string, clr color.Color) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	drawer, err := t.getDrawer(canvas, clr)
	if err != nil {
		return err
	}

	var maxXWidth fixed.Int26_6
	for _, s := range str {
		if width := drawer.MeasureString(s); width > maxXWidth {
			maxXWidth = width
		}
	}

	yHeight := len(str) * int(math.Floor(t.FontSize+t.LineSpace))

	writeBox, err := AlignPosition(align, bounds, maxXWidth.Floor(), yHeight)
	if err != nil {
		return err
	}

	// lineY represents how much space to add for the newline
	lineY := int(math.Floor(t.FontSize + t.LineSpace))

	startX := writeBox.Min.X + t.XStartCorrection
	y := int(math.Floor(t.FontSize)) + writeBox.Min.Y + t.YStartCorrection
	drawer.Dot = fixed.P(startX, y)

	for _, s := range str {
		drawer.DrawString(s)
		y += lineY + t.YStartCorrection
		drawer.Dot = fixed.P(startX, y)
	}

	return nil
}

// MeasureStrings measures the pixel width of a list of strings
func (t *TextWriter) MeasureStrings(canvas draw.Image, str []string) ([]int, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	lengths := make([]int, len(str))
	drawer, err := t.getDrawer(canvas, color.White)
	if err != nil {
		return nil, err
	}

	for i, s := range str {
		lengths[i] = drawer.MeasureString(s).Ceil()
	}

	return lengths, nil
}

// MaxChars returns the maximum number of characters that can fit a given pixel width
func (t *TextWriter) MaxChars(canvas draw.Image, pixWidth int) (int, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	drawer, err := t.getDrawer(canvas, color.White)
	if err != nil {
		return 0, err
	}

	// Grow a run of "M" one character at a time until it no longer fits,
	// reusing a single builder rather than reallocating the string each pass.
	// The cap guards against a font that reports a zero advance for 'M', which
	// would otherwise loop forever.
	var s strings.Builder
	num := 0
	for num <= pixWidth {
		s.WriteByte('M')
		if drawer.MeasureString(s.String()).Ceil() > pixWidth {
			return num, nil
		}
		num++
	}

	return num, nil
}

// WriteAlignedBoxed writes text aligned within a given bounds and draws a box sized to the text width
func (t *TextWriter) WriteAlignedBoxed(align Align, canvas draw.Image, bounds image.Rectangle, str []string, clr color.Color, boxColor color.Color) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	drawer, err := t.getDrawer(canvas, clr)
	if err != nil {
		return err
	}

	var maxXWidth fixed.Int26_6
	for _, s := range str {
		if width := drawer.MeasureString(s); width > maxXWidth {
			maxXWidth = width
		}
	}

	yHeight := len(str) * int(math.Floor(t.FontSize+t.LineSpace))

	boxAlign, err := AlignPosition(align, bounds, maxXWidth.Ceil(), yHeight)
	if err != nil {
		return err
	}
	draw.Draw(canvas, boxAlign, image.NewUniform(boxColor), image.Point{}, draw.Over)

	writeBox, err := AlignPosition(align, bounds, maxXWidth.Floor(), yHeight)
	if err != nil {
		return err
	}

	// lineY represents how much space to add for the newline
	lineY := int(math.Floor(t.FontSize + t.LineSpace))

	startX := writeBox.Min.X + t.XStartCorrection
	y := int(math.Floor(t.FontSize)) + writeBox.Min.Y + t.YStartCorrection
	drawer.Dot = fixed.P(startX, y)

	for _, s := range str {
		drawer.DrawString(s)
		y += lineY + t.YStartCorrection
		drawer.Dot = fixed.P(startX, y)
	}

	return nil
}

// WriteAlignedColorCodes writes text aligned within a given bounds and draws a box sized to the text width
func (t *TextWriter) WriteAlignedColorCodes(align Align, canvas draw.Image, bounds image.Rectangle, colorChars *ColorChar) error {
	if err := colorChars.validate(); err != nil {
		return err
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	drawer, err := t.getDrawer(canvas, color.White)
	if err != nil {
		return err
	}
	maxXWidth := t.maxWidth(colorChars, drawer)

	yHeight := len(colorChars.Lines) * int(math.Floor(t.FontSize+t.LineSpace))

	if colorChars.BoxClr != nil {
		boxAlign, err := AlignPosition(align, bounds, maxXWidth.Ceil(), yHeight)
		if err != nil {
			return err
		}
		draw.Draw(canvas, boxAlign, image.NewUniform(colorChars.BoxClr), image.Point{}, draw.Over)
	}

	writeBox, err := AlignPosition(align, bounds, maxXWidth.Floor(), yHeight)
	if err != nil {
		return err
	}

	// lineY represents how much space to add for the newline
	lineY := int(math.Floor(t.FontSize + t.LineSpace))

	startX := writeBox.Min.X + t.XStartCorrection
	y := int(math.Floor(t.FontSize)) + writeBox.Min.Y + t.YStartCorrection

	pt := fixed.P(startX, y)
	var prev strings.Builder
	for _, line := range colorChars.Lines {
		prev.Reset()
		for i, char := range line.Chars {
			clr := line.Clrs[i]
			drawer, err := t.getDrawer(canvas, clr)
			if err != nil {
				return err
			}
			prevWidth := drawer.MeasureString(prev.String())
			drawer.Dot = pt
			drawer.Dot.X = prevWidth + drawer.Dot.X
			drawer.DrawString(char)
			prev.WriteString(char)
		}
		y += lineY + t.YStartCorrection
		pt = fixed.P(startX, y)
	}

	return nil
}

// WriteColorCodes writes text aligned within a given bounds and draws a box sized to the text width
func (t *TextWriter) WriteColorCodes(canvas draw.Image, bounds image.Rectangle, colorChars *ColorChar) error {
	if err := colorChars.validate(); err != nil {
		return err
	}

	t.lock.Lock()
	defer t.lock.Unlock()

	if colorChars.BoxClr != nil {
		draw.Draw(canvas, bounds, image.NewUniform(colorChars.BoxClr), image.Point{}, draw.Over)
	}

	// lineY represents how much space to add for the newline
	lineY := int(math.Floor(t.FontSize + t.LineSpace))

	startX := bounds.Min.X + t.XStartCorrection
	y := int(math.Floor(t.FontSize)) + bounds.Min.Y + t.YStartCorrection

	pt := fixed.P(startX, y)
	var prev strings.Builder
	for _, line := range colorChars.Lines {
		prev.Reset()
		for i, char := range line.Chars {
			clr := line.Clrs[i]
			drawer, err := t.getDrawer(canvas, clr)
			if err != nil {
				return err
			}
			prevWidth := drawer.MeasureString(prev.String())
			drawer.Dot = pt
			drawer.Dot.X = prevWidth + drawer.Dot.X
			drawer.DrawString(char)
			prev.WriteString(char)
		}
		y += lineY + t.YStartCorrection
		pt = fixed.P(startX, y)
	}

	return nil
}

// maxWidth finds the max width of a slice of ColoChar
func (t *TextWriter) maxWidth(clrChars *ColorChar, drawer *font.Drawer) fixed.Int26_6 {
	var m fixed.Int26_6
	for _, line := range clrChars.Lines {
		str := strings.Join(line.Chars, "")
		if width := drawer.MeasureString(str); width > m {
			m = width
		}
	}

	return m
}

func (c *ColorChar) validate() error {
	for _, line := range c.Lines {
		if len(line.Chars) != len(line.Clrs) {
			return fmt.Errorf("number of chars and colors must match")
		}
	}

	return nil
}

// BreakText breaks text into lines based on a max pixel width
func (t *TextWriter) BreakText(canvas draw.Image, maxPixWidth int, text string) ([]string, error) {
	maxChar, err := t.MaxChars(canvas, maxPixWidth)
	if err != nil {
		return []string{}, err
	}

	return breakText(maxChar, text), nil
}

func breakText(maxLineLen int, text string) []string {
	lines := [][]string{}
	lines = append(lines, []string{})
	words := strings.Fields(text)

	lineIndex := 0
	num := 0
	for i, s := range words {
		if num+len(s) >= maxLineLen {
			lines = append(lines, []string{})
			lines[lineIndex+1] = append(lines[lineIndex+1], s)
			lineIndex++
			num = len(s)
		} else {
			lines[lineIndex] = append(lines[lineIndex], s)
			num += len(s)
			if i != 0 && i != len(words)-1 {
				num++
			}
		}
	}

	retLines := []string{}

	for _, line := range lines {
		nonEmpty := false
		for _, l := range line {
			if l != "" {
				nonEmpty = true
			}
		}
		if !nonEmpty {
			continue
		}
		retLines = append(retLines, strings.Join(line, " "))
	}

	return retLines
}
