package textboard

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io/fs"
	"strings"

	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/logo"
	"github.com/parandandrd/sportsmatrix/internal/rgbrender"
)

const logoCacheDir = "/tmp/sportsmatrix_logos/newslogos"

func (s *TextBoard) renderLogo(ctx context.Context, canvas board.Canvas) error {
	s.Lock()
	defer s.Unlock()

	zeroed := rgbrender.ZeroedBounds(canvas.Bounds())

	if s.config.halfSizeLogo {
		var err error
		zeroed, err = rgbrender.AlignPosition(rgbrender.CenterCenter, zeroed, zeroed.Dx()/2, zeroed.Dy()/2)
		if err != nil {
			return err
		}
	}

	key := fmt.Sprintf("%s_%dx%d", strings.ReplaceAll(s.api.HTTPPathPrefix(), "/", ""), zeroed.Dx(), zeroed.Dy())
	l, ok := s.logos[key]
	if !ok {
		g := func(ctx context.Context) (image.Image, error) {
			return s.api.GetLogo(ctx)
		}
		l = logo.New(key, g, logoCacheDir, zeroed, &logo.Config{
			Abbrev: "news",
			XSize:  zeroed.Dx(),
			YSize:  zeroed.Dy(),
			Pt: &logo.Pt{
				X:    0,
				Y:    0,
				Zoom: 1,
			},
		})
		// Without this the map stays empty, a fresh Logo is built on every
		// render, and its in-memory thumbnail is never reused -- so each
		// headline re-read and re-decoded the thumbnail from disk. The sport
		// board already stores its logos this way.
		s.logos[key] = l
	}

	i, err := l.GetThumbnail(ctx, zeroed)
	if err != nil {
		// As in the sport board: a league with no bundled logo renders its
		// headlines without one instead of erroring every cycle.
		if errors.Is(err, fs.ErrNotExist) {
			s.log.Warn("no logo asset for league, rendering headlines without it",
				zap.String("league", s.api.HTTPPathPrefix()),
			)
			return nil
		}
		return err
	}

	// The thumbnail keeps its aspect ratio, so a square logo comes back 32x32
	// on a 64x32 panel. Drawing it into zeroed put its top-left at the panel's
	// top-left and pinned it to the left edge; place it in the middle instead.
	tb := i.Bounds()
	offX := (zeroed.Dx() - tb.Dx()) / 2
	offY := (zeroed.Dy() - tb.Dy()) / 2
	centered := image.Rect(
		zeroed.Min.X+offX,
		zeroed.Min.Y+offY,
		zeroed.Min.X+offX+tb.Dx(),
		zeroed.Min.Y+offY+tb.Dy(),
	)

	draw.Draw(canvas, centered, i, tb.Min, draw.Over)

	return nil
}

func (s *TextBoard) doRender(canvas board.Canvas, text string) error {
	zeroed := rgbrender.ZeroedBounds(canvas.Bounds())
	lengths, err := s.writer.MeasureStrings(canvas, []string{text})
	if err != nil {
		return err
	}
	if len(lengths) < 1 {
		return fmt.Errorf("failed to measure text")
	}
	bounds := image.Rect(zeroed.Min.X, zeroed.Min.Y, zeroed.Min.X+lengths[0], zeroed.Max.Y)

	s.log.Debug("writing headline",
		zap.String("text", text),
		zap.Int("pix length", lengths[0]),
		zap.Int("X", bounds.Min.X),
		zap.Int("Y", bounds.Min.Y),
		zap.Int("X", bounds.Max.X),
		zap.Int("Y", bounds.Max.Y),
		zap.Int("canvas X", zeroed.Min.X),
		zap.Int("canvas Y", zeroed.Min.Y),
		zap.Int("canvas X", zeroed.Max.X),
		zap.Int("canvas Y", zeroed.Max.Y),
	)

	canvas.SetWidth(bounds.Dx())

	_ = s.writer.WriteAligned(
		rgbrender.CenterCenter,
		canvas,
		bounds,
		[]string{text},
		color.White,
	)

	return nil
}
