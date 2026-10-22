package espnboard

import "testing"

func TestBestLogo(t *testing.T) {
	t.Parallel()

	nfl := []*Logo{
		{Href: "https://a.espncdn.com/i/teamlogos/nfl/500/buf.png", Rel: []string{"full", "default"}},
		{Href: "https://a.espncdn.com/i/teamlogos/nfl/500-dark/buf.png", Rel: []string{"full", "dark"}},
		{Href: "https://a.espncdn.com/i/teamlogos/nfl/500/scoreboard/buf.png", Rel: []string{"full", "scoreboard"}},
		{Href: "https://a.espncdn.com/i/teamlogos/nfl/500-dark/scoreboard/buf.png", Rel: []string{"full", "scoreboard", "dark"}},
	}

	for _, tc := range []struct {
		name  string
		logos []*Logo
		want  string
	}{
		{"scoreboard dark first", nfl, nfl[3].Href},
		{"dark over scoreboard", nfl[:3], nfl[1].Href},
		{"dark over default", nfl[:2], nfl[1].Href},
		{"only default", nfl[:1], nfl[0].Href},
		{
			"no rel, going by href",
			[]*Logo{{Href: nfl[0].Href}, {Href: nfl[1].Href}},
			nfl[1].Href,
		},
		{"none", nil, ""},
		{"nil entry", []*Logo{nil, nfl[0]}, nfl[0].Href},
	} {
		if got := bestLogo(tc.logos); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
