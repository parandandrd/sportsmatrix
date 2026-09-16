package espnboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Headlines ...
type Headlines struct {
	leaguer        Leaguer
	log            *zap.Logger
	updateInterval time.Duration
	lastUpdate     time.Time
	lastHeadlines  []string
	sync.Mutex
}

type news struct {
	Articles []*struct {
		Headline string `json:"headline"`
		Type     string `json:"type"`
	} `json:"articles"`
}

// NewHeadlines ...
func NewHeadlines(leaguer Leaguer, logger *zap.Logger) *Headlines {
	return &Headlines{
		leaguer:        leaguer,
		log:            logger,
		updateInterval: 1 * time.Hour,
	}
}

// HTTPPathPrefix ...
func (h *Headlines) HTTPPathPrefix() string {
	return h.leaguer.HTTPPathPrefix()
}

// GetLogo ...
// GetLogo decodes this league's bundled logo asset.
//
// The result is deliberately not cached here. The only caller is the logo
// package's source getter, which is invoked once to build a thumbnail and then
// caches that thumbnail in memory and on disk. Holding the decoded source as
// well kept a full-size image alive for the life of the process for no further
// use -- 179MB across the bundled league logos, on a board with 1GB of RAM.
func (h *Headlines) GetLogo(ctx context.Context) (image.Image, error) {
	assetfile := fmt.Sprintf("assets/league_logos/%s.png", strings.ToLower(h.leaguer.HTTPPathPrefix()))

	dat, err := assets.ReadFile(assetfile)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(dat)

	l, err := png.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("failed to decode logo for %s: %w", h.leaguer.League(), err)
	}

	return l, nil
}

// GetText ...
func (h *Headlines) GetText(ctx context.Context) ([]string, error) {
	path := h.leaguer.HeadlinePath()
	if len(h.lastHeadlines) > 0 && time.Since(h.lastUpdate) < h.updateInterval {
		return h.lastHeadlines, nil
	}

	uri, err := url.Parse(fmt.Sprintf("http://site.api.espn.com/apis/site/v2/sports/%s", path))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", uri.String(), nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	client := http.DefaultClient

	h.log.Info("Updating headlines from API",
		zap.String("url", uri.String()),
	)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	dat, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var n *news
	if err := json.Unmarshal(dat, &n); err != nil {
		return nil, err
	}

	type headline struct {
		str      string
		priority int
	}
	headlines := []headline{}

	for _, article := range n.Articles {
		t := strings.ToLower(article.Type)

		head := headline{
			str: article.Headline,
		}
		switch t {
		case "headlinenews":
			head.priority = 1
		case "story":
			head.priority = 2
		default:
			head.priority = 3
		}

		h.log.Debug("adding headline",
			zap.Int("priority", head.priority),
			zap.String("headline", head.str),
		)
		headlines = append(headlines, head)
	}

	// sort headlines based on priority
	sort.SliceStable(headlines, func(i int, j int) bool {
		return headlines[i].priority < headlines[j].priority
	})

	h.lastHeadlines = make([]string, len(headlines))

	index := 0
	for _, head := range headlines {
		h.lastHeadlines[index] = head.str
		index++
	}

	h.lastUpdate = time.Now()

	return h.lastHeadlines, nil
}
