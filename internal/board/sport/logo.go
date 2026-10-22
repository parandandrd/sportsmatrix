package sportboard

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"math"

	"go.uber.org/zap"

	"github.com/parandandrd/sportsmatrix/internal/logo"
	"github.com/parandandrd/sportsmatrix/internal/rgbrender"
)

func (s *SportBoard) logoConfig(logoKey string, bounds image.Rectangle) *logo.Config {
	for _, conf := range s.config.LogoConfigs {
		if conf.Abbrev == logoKey {
			return conf
		}
	}

	s.log.Debug("no logo config defined, defaults will be used", zap.String("logo key", logoKey))

	return &logo.Config{
		Abbrev: logoKey,
		XSize:  bounds.Dx(),
		YSize:  bounds.Dy(),
		Pt: &logo.Pt{
			X:    0,
			Y:    0,
			Zoom: 1,
		},
	}
}

func (s *SportBoard) clearDrawCache() {
	s.drawLock.Lock()
	defer s.drawLock.Unlock()
	s.logoDrawCache = make(map[string]image.Image)
}

func (s *SportBoard) getLogoDrawCache(logoKey string) (image.Image, error) {
	s.drawLock.RLock()
	defer s.drawLock.RUnlock()
	l, ok := s.logoDrawCache[logoKey]
	if ok {
		if l == nil {
			// Deliberately not deleting the entry here: this only holds a read
			// lock, and writing to the map while another reader holds it is a
			// fatal runtime throw. Reporting a miss is enough -- the caller
			// re-renders and setLogoDrawCache overwrites the nil entry.
			s.log.Warn("logo draw cache was nil",
				zap.String("league", s.api.League()),
				zap.String("key", logoKey),
			)
			return nil, fmt.Errorf("no cache for %s", logoKey)
		}
		return l, nil
	}

	return nil, fmt.Errorf("no cache for %s", logoKey)
}

func (s *SportBoard) setLogoDrawCache(logoKey string, img image.Image) {
	s.drawLock.Lock()
	defer s.drawLock.Unlock()

	s.log.Debug("set logo draw cache",
		zap.String("league", s.api.League()),
		zap.String("key", logoKey),
	)
	s.logoDrawCache[logoKey] = img
}

func (s *SportBoard) setLogoCache(logoKey string, l *logo.Logo) {
	s.logoLock.Lock()
	defer s.logoLock.Unlock()

	s.log.Debug("set logo cache",
		zap.String("league", s.api.League()),
		zap.String("key", logoKey),
	)
	s.logos[logoKey] = l
}

func (s *SportBoard) getLogoCache(logoKey string) (*logo.Logo, error) {
	s.logoLock.RLock()
	defer s.logoLock.RUnlock()

	l, ok := s.logos[logoKey]
	if ok {
		if l == nil {
			// See getLogoDrawCache: no map write under the read lock.
			s.log.Warn("logo cache was nil",
				zap.String("league", s.api.League()),
				zap.String("key", logoKey),
			)
			return nil, fmt.Errorf("no cache for %s", logoKey)
		}
		return l, nil
	}

	s.log.Debug("no logo cache",
		zap.String("league", s.api.League()),
		zap.String("key", logoKey),
	)
	return nil, fmt.Errorf("no cache for %s", logoKey)
}

// RenderLeftLogo ...
func (s *SportBoard) RenderLeftLogo(ctx context.Context, canvasBounds image.Rectangle, teamID string) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, context.Canceled
	default:
	}
	bounds := rgbrender.ZeroedBounds(canvasBounds)
	logoKey := fmt.Sprintf("%s_HOME_%dx%d", teamID, bounds.Dx(), bounds.Dy())

	i, err := s.getLogoDrawCache(logoKey)
	if err == nil && i != nil {
		s.log.Debug("drawing logo with drawCache", zap.String("logo key", logoKey))
		return i, nil
	}

	l, err := s.getLogoCache(logoKey)
	if err != nil {
		var err error
		logoConf := s.logoConfig(logoKey, bounds)

		s.log.Debug("fetching logo",
			zap.String("logoKey", logoKey),
			zap.Int("X", bounds.Dx()),
			zap.Int("Y", bounds.Dy()),
		)
		l, err = s.api.GetLogo(ctx, logoKey, logoConf, bounds)
		if err != nil {
			s.log.Error("failed to get left logo", zap.Error(err))
			return nil, fmt.Errorf("failed to get left logo: %w", err)
		}
		l.SetLogger(s.log)
		s.setLogoCache(logoKey, l)
	} else {
		s.log.Debug("using logo cache", zap.String("logo key", logoKey))
	}

	if l == nil {
		s.log.Error("logo was nil",
			zap.String("league", s.api.League()),
			zap.String("key", logoKey),
		)
		return nil, fmt.Errorf("logo %s was nil", logoKey)
	}

	renderedLogo, setCache, err := s.placeLogo(ctx, l, logoKey, bounds, teamID, false)
	if err != nil {
		s.log.Error("failed to render left logo", zap.Error(err))
		return nil, fmt.Errorf("failed to render left logo: %w", err)
	}
	if setCache {
		s.setLogoDrawCache(logoKey, renderedLogo)
	}
	return renderedLogo, nil
}

// RenderRightLogo ...
func (s *SportBoard) RenderRightLogo(ctx context.Context, canvasBounds image.Rectangle, teamID string) (image.Image, error) {
	select {
	case <-ctx.Done():
		return nil, context.Canceled
	default:
	}
	bounds := rgbrender.ZeroedBounds(canvasBounds)
	logoKey := fmt.Sprintf("%s_AWAY_%dx%d", teamID, bounds.Dx(), bounds.Dy())

	i, err := s.getLogoDrawCache(logoKey)
	if err == nil && i != nil {
		s.log.Debug("drawing logo with drawCache", zap.String("logo key", logoKey))
		return i, nil
	}

	l, err := s.getLogoCache(logoKey)
	if err != nil {
		var err error
		logoConf := s.logoConfig(logoKey, bounds)

		s.log.Debug("fetching logo",
			zap.String("abbreviation", teamID),
			zap.Int("X", bounds.Dx()),
			zap.Int("Y", bounds.Dy()),
		)
		l, err = s.api.GetLogo(ctx, logoKey, logoConf, bounds)
		if err != nil {
			s.log.Error("failed to get right logo", zap.Error(err))
			return nil, fmt.Errorf("failed to get right logo: %w", err)
		}
		l.SetLogger(s.log)
		s.setLogoCache(logoKey, l)
	}

	if l == nil {
		s.log.Error("logo was nil",
			zap.String("league", s.api.League()),
			zap.String("key", logoKey),
		)
		return nil, fmt.Errorf("logo %s was nil", logoKey)
	}

	renderedLogo, setCache, err := s.placeLogo(ctx, l, logoKey, bounds, teamID, true)
	if err != nil {
		s.log.Error("failed to render right logo", zap.Error(err))
		return nil, fmt.Errorf("failed to render right logo: %w", err)
	}
	if setCache {
		s.setLogoDrawCache(logoKey, renderedLogo)
	}
	return renderedLogo, nil
}

// logoArea is how wide a team's logo may be, from the panel's outer edge
// towards the score. ok is false if that depends on a record width that could
// not be worked out, so the result should not be cached.
func (s *SportBoard) logoArea(bounds image.Rectangle, teamID string) (int, bool) {
	width := (bounds.Dx() - s.textAreaWidth(bounds)) / 2
	ok := true

	if s.config.ShowRecord.Load() {
		w, err := s.getTeamInfoWidth(s.api.League(), teamID)
		if err != nil {
			s.log.Error("failed to get team info width", zap.Error(err))
			w = defaultTeamInfoArea
			ok = false
		}
		return width - w, ok
	}

	// The gradient darkens what is under the score so it stays readable, so
	// a logo can run a pixel under its edge. On a 64x32 panel that makes the
	// area 28 wide, as tall as the logo is allowed to be.
	if s.config.UseGradient.Load() {
		width++
	}
	return width, ok
}

// placeLogo draws a team's logo for one side of the score: trimmed of its
// transparent margins, as large as fits in its side's area and the panel's
// height less a margin top and bottom, centred in both. The image it returns
// is in panel coordinates.
//
// A logo config's zoom scales that area, and its shifts move the logo after
// it is placed. A zoom over 1 can make a logo wider than its area; it then
// runs off the panel's outer edge rather than under the score.
func (s *SportBoard) placeLogo(ctx context.Context, l *logo.Logo, logoKey string, bounds image.Rectangle, teamID string, right bool) (image.Image, bool, error) {
	conf := s.logoConfig(logoKey, bounds)

	area, cacheable := s.logoArea(bounds, teamID)
	maxW := area
	maxH := bounds.Dy() - 2*(bounds.Dy()/16)
	if conf.Pt != nil && conf.Pt.Zoom > 0 {
		maxW = int(math.Round(float64(maxW) * conf.Pt.Zoom))
		maxH = int(math.Round(float64(maxH) * conf.Pt.Zoom))
	}
	if maxW < 1 || maxH < 1 {
		return nil, false, fmt.Errorf("no room for logo %s: %dx%d", logoKey, maxW, maxH)
	}

	thumb, err := l.Fit(ctx, maxW, maxH)
	if err != nil {
		return nil, false, err
	}
	w, h := thumb.Bounds().Dx(), thumb.Bounds().Dy()

	x, y := logoPos(bounds, area, w, h, right)
	if conf.Pt != nil {
		x += conf.Pt.X
		y += conf.Pt.Y
	}

	s.log.Debug("place logo",
		zap.String("logo key", logoKey),
		zap.Int("area", area),
		zap.Int("x", x),
		zap.Int("y", y),
		zap.Int("width", w),
		zap.Int("height", h),
	)

	i := image.NewRGBA(image.Rect(x, y, x+w, y+h))
	draw.Draw(i, i.Bounds(), thumb, thumb.Bounds().Min, draw.Src)

	return i, cacheable, nil
}

// logoPos is where a w x h logo goes on a panel of the given bounds: centred
// in the area wide strip along the left edge, or the right one, and centred
// top to bottom. One wider than its strip runs off the panel's outer edge.
func logoPos(bounds image.Rectangle, area, w, h int, right bool) (int, int) {
	x := (area - w) / 2
	if w > area {
		x = area - w
	}
	if right {
		x = bounds.Dx() - x - w
	}
	return x, (bounds.Dy() - h) / 2
}
