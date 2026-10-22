package espnboard

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"go.uber.org/zap"
	"gopkg.in/yaml.v2"

	"github.com/parandandrd/sportsmatrix/internal/logo"
	"github.com/parandandrd/sportsmatrix/internal/rgbrender"
	"github.com/parandandrd/sportsmatrix/internal/util"
)

// sourceCacheVersion is part of each cached source logo's file name. Bump it
// when the choice of logo changes, so the Pi fetches the new one instead of
// keeping the old.
const sourceCacheVersion = 2

func (e *ESPNBoard) getLogoCache(logoKey string) (*logo.Logo, error) {
	e.logoLock.RLock()
	defer e.logoLock.RUnlock()

	l, ok := e.logos[logoKey]
	if ok {
		return l, nil
	}

	return l, fmt.Errorf("no cache for logo %s", logoKey)
}

func (e *ESPNBoard) setLogoCache(logoKey string, l *logo.Logo) {
	e.logoLock.Lock()
	defer e.logoLock.Unlock()

	e.logos[logoKey] = l
}

// GetLogo ...
func (e *ESPNBoard) GetLogo(ctx context.Context, logoKey string, logoConf *logo.Config, bounds image.Rectangle) (*logo.Logo, error) {
	logoKey = fmt.Sprintf("%s_%s", e.leaguer.HTTPPathPrefix(), logoKey)
	if l, err := e.getLogoCache(logoKey); err == nil {
		return l, nil
	}

	cacheDir, err := e.logoCacheDir()
	if err != nil {
		return nil, err
	}

	// A logoKey should be LEAGUE_TEAM_HOME|AWAY_XxY, ie. ncaam_1234_HOME_64x32
	p := strings.Split(logoKey, "_")
	if len(p) < 4 {
		return nil, fmt.Errorf("invalid logo key")
	}

	teamID := p[1]
	dimKey := p[3]

	e.logoLock.Lock()
	_, ok := e.logoConfOnce[dimKey]
	if !ok {
		e.log.Debug("loading default logo configs",
			zap.Int("x", bounds.Dx()),
			zap.Int("y", bounds.Dy()),
		)
		if err := e.loadDefaultLogoConfigs(bounds); err != nil {
			// Log the error, but don't return. We'll just use defaults
			e.log.Warn("no defaults defined for logos", zap.String("league", e.leaguer.League()))
		}
		e.logoConfOnce[dimKey] = struct{}{}
	}
	e.logoLock.Unlock()

	var l *logo.Logo
	defer e.setLogoCache(logoKey, l)

	logoGetter := func(ctx context.Context) (image.Image, error) {
		return e.GetLogoSource(ctx, teamID)
	}

	if logoConf != nil {
		l = logo.New(logoKey, logoGetter, cacheDir, bounds, logoConf)

		return l, nil
	}

	for _, d := range *e.defaultLogoConf {
		if d.Abbrev == logoKey {
			l = logo.New(logoKey, logoGetter, cacheDir, bounds, d)

			return l, nil
		}
	}

	c := &logo.Config{
		Abbrev: logoKey,
		XSize:  bounds.Dx(),
		YSize:  bounds.Dy(),
		Pt: &logo.Pt{
			X:    0,
			Y:    0,
			Zoom: 1,
		},
	}

	*e.defaultLogoConf = append(*e.defaultLogoConf, c)

	l = logo.New(logoKey, logoGetter, cacheDir, bounds, c)

	return l, nil
}

func (e *ESPNBoard) loadDefaultLogoConfigs(bounds image.Rectangle) error {
	dat, err := assets.ReadFile(fmt.Sprintf("assets/logopos_%dx%d.yaml", bounds.Dx(), bounds.Dy()))
	if err != nil {
		return err
	}

	var confs []*logo.Config
	if err := yaml.Unmarshal(dat, &confs); err != nil {
		return err
	}
	*e.defaultLogoConf = append(*e.defaultLogoConf, confs...)

	return nil
}

// logoRank says how well one of ESPN's versions of a logo suits the panel;
// higher is better. ESPN draws a "dark" version of most logos for dark
// backgrounds, with light outlines where the default has navy or black ones
// that vanish on the unlit panel. Its "scoreboard" versions are simplified
// for small sizes. Both together is best, then dark alone.
func logoRank(l Logo) int {
	rank := 0
	if slices.Contains(l.Rel, "dark") || strings.Contains(l.Href, "-dark/") {
		rank += 2
	}
	if slices.Contains(l.Rel, "scoreboard") || strings.Contains(l.Href, "/scoreboard/") {
		rank++
	}
	return rank
}

// bestLogo picks the href of the version of a team's logo that suits the
// panel best, the first listed among equals. It is empty if there are none.
func bestLogo(logos []*Logo) string {
	best, href := -1, ""
	for _, l := range logos {
		if l == nil || l.Href == "" {
			continue
		}
		if r := logoRank(*l); r > best {
			best, href = r, l.Href
		}
	}
	return href
}

// GetLogoSource ...
func (e *ESPNBoard) GetLogoSource(ctx context.Context, teamID string) (image.Image, error) {
	e.Lock()
	l, ok := e.logoLockers[teamID]
	if !ok {
		e.logoLockers[teamID] = &sync.Mutex{}
		l = e.logoLockers[teamID]
	}
	e.Unlock()
	l.Lock()
	defer l.Unlock()

	cacheDir, err := e.logoCacheDir()
	if err != nil {
		return nil, err
	}

	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("%s_%s_v%d.png", e.leaguer.HTTPPathPrefix(), teamID, sourceCacheVersion))

	if _, err := os.Stat(cacheFile); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to detect if logo cache file exists: %w", err)
		}
	} else {
		e.log.Debug("reading source logo from cache",
			zap.String("league", e.leaguer.League()),
			zap.String("team", teamID),
		)
		r, err := os.Open(cacheFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open logo cache file: %w", err)
		}
		return png.Decode(r)
	}

	teams, err := e.getTeams(ctx)
	if err != nil || len(teams) < 1 {
		return nil, fmt.Errorf("failed to fetch team info for logo: %w", err)
	}
	e.log.Debug("looking for logo source",
		zap.String("league", e.leaguer.League()),
		zap.String("team", teamID),
		zap.Int("num teams", len(teams)),
	)

	var i image.Image
	foundSource := false
	for _, team := range teams {
		if team.ID != teamID {
			continue
		}

		e.log.Debug("looking for logo source",
			zap.String("league", e.leaguer.League()),
			zap.String("team", teamID),
			zap.Int("num logos", len(team.Logos)),
		)

		href := bestLogo(team.Logos)
		if href == "" {
			return nil, fmt.Errorf("team %s has no logo", teamID)
		}

		e.log.Debug("pulling logo from API",
			zap.String("URL", href),
			zap.String("league", e.leaguer.League()),
			zap.String("team", teamID),
		)
		i, err = util.PullPng(ctx, href)
		if err != nil || i == nil {
			return nil, fmt.Errorf("failed to retrieve logo from API for %s %s: %w", teamID, href, err)
		}
		i = logo.ShrinkSource(i)
		foundSource = true
	}

	if !foundSource {
		return nil, fmt.Errorf("failed to determine source URL for logo %s", teamID)
	}

	e.log.Debug("saving source logo to cache",
		zap.String("league", e.leaguer.League()),
		zap.String("team", teamID),
	)
	if err := rgbrender.SavePng(i, cacheFile); err != nil {
		e.log.Error("failed to save logo to cache", zap.Error(err))
		_ = os.Remove(cacheFile)
	}

	return i, nil
}
