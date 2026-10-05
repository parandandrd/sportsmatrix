# Plan: what's next

Rewritten 2026-10-05 after v0.0.8. Read `CLAUDE.md` first; this file only adds
what is specific to the work below. Delete items as they get done, and the
file when nothing is left.

## Where things stand

Released on 2026-10-04/05 (all rebase-merged, linear history):

| PR | What | Released in |
|---|---|---|
| #22 | Go 1.23 -> 1.27, golangci-lint v1 -> v2.14.0 | v0.0.4-beta.11 |
| #23 | Vendored matrix library -> hzeller `51d3231` | v0.0.4-beta.11 |
| #24 | Removed the stats boards, PGA, XFL, `internal/nhl`, `internal/mlb`, `--alt-api` | v0.0.5 |
| #25 | Actions to v7, Node 24, Go deps within their majors | v0.0.6 |
| #26 | Protos on `emptypb` (no `golang/protobuf`), unused codegen tools and `Dockerfile.protoc` gone, npm updates in range | v0.0.7 |
| #27 | Web UI on Vite 8 + Vitest instead of Create React App; board components post plain objects (no `google-protobuf`); react-router 7; no `--legacy-peer-deps`; `npm audit` 88 -> 0 | v0.0.8 |
| #28 | ESLint 10 + react-hooks rules on `web/`, run in CI | v0.0.8 |

Go modules linked into the binary are at their latest minor/patch (apart from
"Deliberately left alone"); the web UI's dependencies are at their latest, and
its dev tools at their latest except `@testing-library/jest-dom` (6.9; 7.0 is
out and untried). Go 1.27.1, Node 24, CI actions at their latest majors.

On the Pi: the owner installed #22's build and v0.0.4-beta.11 and the panel
looked fine. Nothing since (v0.0.5 to v0.0.8) has been reported on.

## Work, in order

### 1. Try v0.0.8 on the Pi (the owner)

v0.0.8 is the first release with the Vite UI, and its board components send
switches differently (plain objects instead of protobuf messages). It was
checked in Chromium against the Go binary in `--test` mode, but never on the Pi
or in a phone browser. Worth looking at:

- the dashboard and `/board`, including **Full screen** (never clicked by
  anyone; it is offered only where the browser allows it, not iPhone Safari)
- a board page (`/b/<name>`): flip a switch, reload, and check it stuck, and
  that `/etc/sportsmatrix.conf` changed by one line
- the API docs page
- the panel itself, after the v0.0.5-v0.0.8 Go changes

Whatever is confirmed belongs in `CLAUDE.md`'s "State of things"; anything
broken comes before everything below.

### 2. Board pruning (waiting on the owner)

The owner is deciding which boards they actually use. Boards left after #24:
NFL, NBA, MLB (with its ESPN live view), NHL, WNBA, NCAAF, NCAAM, NCAAW; MLS,
NWSL, EPL, Bundesliga (`dfl`), DFB-Pokal (`dfb`), UEFA, Ligue 1, Serie A, La
Liga, FIFA; F1, IndyCar (`irl`); weather, TV, clock, sys, image. Each league
also has an optional headlines ticker.

#24 is the template for removing one. The places it touched:

- `cmd/sportsmatrix/main.go`: the `setConfigDefaults` block and the
  `getBoards` block
- `internal/config/config.go`: the config field
- `internal/espnboard/leagues.go`: the league type and its `GetLeaguer` case,
  plus `internal/espnboard/assets/<league>_teams.json` and the matching line
  in `script/update_team_assets`
- `sportsmatrix.conf.example`: the section
- `web/src/Logo.js` and the logo png
- `README.md`: the list and any screenshot
- Tests that use a section as an example: `internal/conffile/conffile_test.go`
  hard-codes section positions (`keys[21]`) and names; also
  `cmd/sportsmatrix/sections_test.go` and
  `internal/sportsmatrix/settings_test.go`

Old config files keep loading: `ghodss/yaml` ignores unknown keys. The Pi's
`/etc/sportsmatrix.conf` probably still has `xflConfig:`, `pga:` and `stats:`
sections from before #24; harmless, and the owner can delete them.

### 3. Small cleanups, each its own PR, any order

- **Regenerate `web/src/matrix.swagger.json`.** It is behind the protos
  (`SearchShows` and the newer `BoardSettings` fields are missing), and every
  Empty `$ref` points at a `<pkg>_google.protobuf.Empty` definition that
  doesn't exist. `./script/doc-gen` (needs `jq`) regenerates it, but its
  `sed` rewrites are stale too: BasicBoard paths become `/stocks/...` (there
  is no stocks board) and the Sport description lists leagues by hand. Fix
  the script, regenerate, and check the API docs page in a browser.
- **README images.** It shows a "Stock Ticker" screenshot (line ~265) for a
  board that doesn't exist, and four UI screenshots (`ui1.png`-`ui4.png`,
  line ~232) that probably predate the current dashboard. `sysboard.jpg`,
  `tv_mlb.jpg`, `tv_nhl_stats.jpg` and `webboard.png` in `assets/images/` are
  referenced nowhere. New UI screenshots can come from the `--test` binary
  and Playwright (see the sandbox notes).
- **`react-hooks/set-state-in-effect`** is off in `web/eslint.config.js`. It
  flags six effects in `Dashboard.jsx`, `BoardSettings.jsx` and
  `MatrixSettings.jsx` that copy a prop into state or load data on mount.
  They work; satisfying the rule means restructuring them (a `key` to reset
  a draft, deriving instead of copying). Do it only together with turning the
  rule on, and with a component test for each.
- **Optional: a dev proxy.** `npm start` serves the UI with no backend: the
  app posts to whatever host served it. A `server.proxy` in
  `web/vite.config.js` to a Pi (or the `--test` binary) would make the dev
  server useful. The Twirp paths have per-board prefixes (`/nhl/sport.v1...`),
  so the proxy has to forward everything that isn't a Vite module.
- **Release notes.** v0.0.5's auto-generated notes list only #24; #22 and
  #23 shipped in it too. Fix by hand on the release page if it matters.

### 4. Ideas, only if the owner wants them

- The XFL's successor, the UFL, has an ESPN feed (`football/ufl`). Adding it
  is roughly the XFL code #24 deleted.
- `eslint-plugin-react` once it supports ESLint 10, for the JSX-specific
  rules (keys in lists, etc.).

## Deliberately left alone

These are unmaintained but working, and are linked into the binary:
`golang/freetype` (2017), `nfnt/resize` (archived), `disintegration/imaging`,
`ghodss/yaml`, `gopkg.in/yaml.v2`/`v3` (archived), `robfig/cron/v3`.

None has a known issue that matters here. Replacing them means rewriting text
and logo rendering, or the config parsing. Config parsing in particular relies
on `ghodss/yaml`'s YAML 1.1 behaviour (a plain `NO` is `false`) and on
`internal/conffile`'s edit-and-verify. Revisit only with a reason.

## Things that will save time in the sandbox

- **Go:** `GOTOOLCHAIN=go1.27.1` downloads the toolchain through the module
  proxy.
- **golangci-lint:** `./script/install-golangci-lint <dir>` installs v2.14.0.
  The sandbox's preinstalled 2.5.0 is built with Go 1.25 and refuses this
  module.
- **protoc:** `apt-get install -y protobuf-compiler` works and gives
  3.21.12, the version the committed files name, with `empty.proto` under
  `/usr/include`.
- **Node 24:** `https://nodejs.org/dist/latest-v24.x/` plus its
  `SHASUMS256.txt`. Its npm 11 warns that tree-sitter's install scripts were
  not run (swagger-ui's dependency); the tests and build don't need them.
- **Trying the web UI against the real API:** build `web/`, delete
  `internal/sportsmatrix/assets/web`, `BUILDARCH=x86_64 ./script/build`, then
  run `sportsmatrix.x86_64 run --test -c <copy of sportsmatrix.conf.example
  with httpListenPort changed>`. The console matrix stands in for the panel
  and settings are saved to that copy. Playwright is at
  `/opt/node-tools/node_modules/playwright`, browsers via
  `PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers`. Wait on `load`, not
  `networkidle`: the dashboard long-polls frames. Don't `pkill -f` a pattern
  that also appears in your own command line; it kills the shell.
- **ESPN and other data APIs are blocked** from the sandbox. GitHub release
  downloads, nodejs.org, go.dev's module proxy and the Go toolchain work.
- **Bringing third-party source into the repo** (as #23 did with the matrix
  library) was blocked by the session's auto-mode safety check until the owner
  said to go ahead. Ask before doing it.
- **After a rebase-merge** the work branch holds pre-rebase copies of the
  merged commits. Reset it to `origin/master` and push with
  `--force-with-lease` before starting the next PR.
- **CI's Go `build` job can sit queued with no runner** (it happened on #26:
  15 minutes, then cancelled before any step ran). Check the job's
  `runner_id`; 0 means it never started, which is not a test result.
- **Releases:** run `release.yml` via workflow_dispatch with `version`. A
  version containing `beta` is marked prerelease; anything else becomes
  "Latest".
- **On the Pi:** `sudo dpkg -i --force-confdef --force-confold
  sportsmatrix-*.deb`. The package is `sportsmatrix-<version>_aarch64.deb`,
  hyphen after the name.
