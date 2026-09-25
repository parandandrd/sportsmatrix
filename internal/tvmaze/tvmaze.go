// Package tvmaze finds TV shows and their coming episodes on TVmaze,
// api.tvmaze.com, which needs no account or key.
package tvmaze

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Show is a TV show.
type Show struct {
	ID   int
	Name string
	// Network is the channel or streaming service it is on.
	Network string
	// Premiered is the year it began, or empty when TVmaze doesn't know.
	Premiered string
	Status    string
}

// Episode is one episode of a show.
type Episode struct {
	Show    Show
	Season  int
	Number  int
	Name    string
	Airs    time.Time
	Runtime time.Duration
	// Timed is false for an episode given only a day, as a streaming service's
	// often are.
	Timed bool
	// Special is an episode outside the regular run, such as a reunion.
	Special bool
}

// Premiere is the first episode of a season.
func (e Episode) Premiere() bool {
	return !e.Special && e.Number == 1
}

// ErrNotFound is a show TVmaze doesn't have.
var ErrNotFound = errors.New("no such show on TVmaze")

// Client asks TVmaze.
type Client struct {
	client *http.Client
	base   string
}

// New returns a TVmaze client. base is the API's address, and only tests need
// to give one.
func New(client *http.Client, base string) *Client {
	if base == "" {
		base = "https://api.tvmaze.com"
	}
	return &Client{client: client, base: strings.TrimRight(base, "/")}
}

// NewHTTPClient is an HTTP client with a timeout suited to TVmaze.
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second}
}

const userAgent = "sportsmatrix (github.com/parandandrd/sportsmatrix)"

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusTooManyRequests:
		return errors.New("TVmaze says too many requests; it will be asked again later")
	default:
		return fmt.Errorf("TVmaze answered %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

type apiShow struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Premiered string `json:"premiered"`
	Status    string `json:"status"`
	Network   *struct {
		Name string `json:"name"`
	} `json:"network"`
	WebChannel *struct {
		Name string `json:"name"`
	} `json:"webChannel"`
	Embedded struct {
		NextEpisode *apiEpisode `json:"nextepisode"`
	} `json:"_embedded"`
}

func (s apiShow) show() Show {
	out := Show{ID: s.ID, Name: s.Name, Status: s.Status}
	if len(s.Premiered) >= 4 {
		out.Premiered = s.Premiered[:4]
	}
	switch {
	case s.Network != nil:
		out.Network = s.Network.Name
	case s.WebChannel != nil:
		out.Network = s.WebChannel.Name
	}
	return out
}

type apiEpisode struct {
	Name     string `json:"name"`
	Season   int    `json:"season"`
	Number   *int   `json:"number"`
	Type     string `json:"type"`
	Airtime  string `json:"airtime"`
	Airstamp string `json:"airstamp"`
	Runtime  *int   `json:"runtime"`
}

func (e apiEpisode) episode(show Show) (Episode, bool) {
	airs, err := time.Parse(time.RFC3339, e.Airstamp)
	if err != nil {
		return Episode{}, false
	}
	out := Episode{
		Show:    show,
		Season:  e.Season,
		Name:    e.Name,
		Airs:    airs,
		Timed:   e.Airtime != "",
		Special: e.Type != "" && e.Type != "regular",
	}
	if e.Number != nil {
		out.Number = *e.Number
	}
	if e.Runtime != nil {
		out.Runtime = time.Duration(*e.Runtime) * time.Minute
	}
	return out, true
}

// Search finds shows by name, best match first.
func (c *Client) Search(ctx context.Context, query string) ([]Show, error) {
	var results []struct {
		Show apiShow `json:"show"`
	}
	if err := c.get(ctx, "/search/shows", url.Values{"q": {query}}, &results); err != nil {
		return nil, err
	}
	out := make([]Show, 0, len(results))
	for _, r := range results {
		out = append(out, r.Show.show())
	}
	return out, nil
}

// Find is the one show TVmaze thinks a name means.
func (c *Client) Find(ctx context.Context, name string) (Show, error) {
	var s apiShow
	if err := c.get(ctx, "/singlesearch/shows", url.Values{"q": {name}}, &s); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Show{}, fmt.Errorf("%w called %q", ErrNotFound, name)
		}
		return Show{}, err
	}
	return s.show(), nil
}

// Show looks up a show by its TVmaze ID, with its next episode when one is
// scheduled.
func (c *Client) Show(ctx context.Context, id int) (Show, *Episode, error) {
	var s apiShow
	if err := c.get(ctx, "/shows/"+strconv.Itoa(id), url.Values{"embed": {"nextepisode"}}, &s); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Show{}, nil, fmt.Errorf("%w with the ID %d", ErrNotFound, id)
		}
		return Show{}, nil, err
	}

	show := s.show()
	if s.Embedded.NextEpisode == nil {
		return show, nil, nil
	}
	next, ok := s.Embedded.NextEpisode.episode(show)
	if !ok {
		return show, nil, nil
	}
	return show, &next, nil
}

// episodes are all of a show's episodes, specials included.
func (c *Client) episodes(ctx context.Context, show Show) ([]Episode, error) {
	var eps []apiEpisode
	if err := c.get(ctx, "/shows/"+strconv.Itoa(show.ID)+"/episodes", url.Values{"specials": {"1"}}, &eps); err != nil {
		return nil, err
	}
	out := make([]Episode, 0, len(eps))
	for _, e := range eps {
		if e.Type == "insignificant_special" {
			// recaps, clips and the like
			continue
		}
		if ep, ok := e.episode(show); ok {
			out = append(out, ep)
		}
	}
	return out, nil
}

// Upcoming are a show's new episodes airing from from until to. Most of the
// time a show has nothing that soon, which its next episode says without
// fetching the whole list: a long-running show's is hundreds of kilobytes.
func (c *Client) Upcoming(ctx context.Context, id int, from, to time.Time) (Show, []Episode, error) {
	show, next, err := c.Show(ctx, id)
	if err != nil {
		return Show{}, nil, err
	}
	if next == nil || next.Airs.After(to) {
		return show, nil, nil
	}

	eps, err := c.episodes(ctx, show)
	if err != nil {
		return show, nil, err
	}

	var out []Episode
	for _, e := range eps {
		if !e.Airs.Before(from) && !e.Airs.After(to) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Airs.Before(out[j].Airs) })
	return show, out, nil
}

// Entry is a show as the config file lists it: "Family Guy (tvmaze 84)", or
// just "Family Guy" for one typed by hand, which is looked up by name.
type Entry struct {
	Name string
	ID   int
}

var entryID = regexp.MustCompile(`\s*\(tvmaze (\d+)\)\s*$`)

// ParseEntry reads a show as the config file lists it.
func ParseEntry(s string) Entry {
	s = strings.TrimSpace(s)
	if m := entryID.FindStringSubmatchIndex(s); m != nil {
		id, err := strconv.Atoi(s[m[2]:m[3]])
		if err == nil && id > 0 {
			return Entry{Name: strings.TrimSpace(s[:m[0]]), ID: id}
		}
	}
	return Entry{Name: s}
}

// String writes an entry as the config file lists it.
func (e Entry) String() string {
	if e.ID == 0 {
		return e.Name
	}
	return fmt.Sprintf("%s (tvmaze %d)", e.Name, e.ID)
}
