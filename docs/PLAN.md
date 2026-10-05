# Plan: dependency cleanup and board pruning

Written 2026-10-05, at the end of a long session, to hand the remaining work to
a fresh one. Read `CLAUDE.md` first; this file only adds what is specific to
the work below. Delete this file, or the items in it, as they get done.

## Where things stand

Merged and released this session (all rebase-merged, linear history):

| PR | What | Released in |
|---|---|---|
| #22 | Go 1.23 -> 1.27 (`go.mod` drives CI and releases), golangci-lint v1.64.6 -> v2.14.0 | v0.0.4-beta.11 |
| #23 | Vendored hzeller matrix library -> upstream `51d3231`, with `-march=native`/LTO off and `disable_busy_waiting` set from Go | v0.0.4-beta.11 |
| #24 | Removed the stats boards, PGA, XFL, `internal/nhl`, `internal/mlb`, `--alt-api` | v0.0.5 (first full release) |
| #25 | Actions to v7, Node 20 -> 24, Go deps updated within their majors | v0.0.6 |
| #26 | Plan item 1: protos on `emptypb` (no `golang/protobuf`), unused codegen tools and `Dockerfile.protoc` gone, npm updates in range | v0.0.7 |

On the Pi: the owner installed the #22 and v0.0.4-beta.11 builds and the panel
looked fine. v0.0.5, v0.0.6 and v0.0.7 have not been reported on.

#26's Go `build` job never got a runner (queued 15 minutes, cancelled before
any step ran); it was merged on local lint and tests, and `release.yml`, which
runs both again, passed for v0.0.7.

Current as of v0.0.7: Go 1.27.1, golangci-lint 2.14.0, every Go module that is
linked into the binary at its latest minor/patch, CI actions at their latest
majors, Node 24, the matrix library at hzeller HEAD.

A correction to #25's description: `golang.org/x/net` and `x/crypto` are not
linked into the binary (`go list -deps ./cmd/sportsmatrix` doesn't list them).
The binary's HTTP and TLS are the standard library's, so those fixes came with
the Go 1.27 bump in #22.

## Work, in order

### 1. Small cleanup PR -- done

Merged as #26, released in v0.0.7.

### 2. Move the web UI off Create React App -- PR #27 open

Branch `claude/nice-brown-bi6dbx`, four commits. **Left to do:** CI green,
the owner tries its arm64 `.deb` on the Pi (and the Full screen button, which
nothing has clicked), rebase-merge, release.

What it does:

- The board components (Sport, Racing, BasicBoard, ImageBoard) post plain
  objects with the .proto field names instead of google-protobuf messages.
  The `web/src/*/*_pb.js` stubs, `google-protobuf` and `script/proto-gen`'s
  `protoc-gen-js` branch are gone. The stubs were CommonJS in `src/`, which
  Vite's dev server can't serve.
- Vite 8 + Vitest 5. Output is still `web/build`; hashed files go under
  `static/` (`build.assetsDir`) so `webui.go`'s immutable cache rule still
  applies. JSX files are `.jsx`. `package.json` lists only what `src/`
  imports; the `>=` pins, `web-vitals` and CRA's leftovers are gone.
- react-router 7.18.4 (imports from `react-router`). `npm audit`: 0.
- `--legacy-peer-deps` is gone from CI, `release.yml` and the docs. Plain
  `npm ci` works; swagger-ui's `react-debounce-input` and `react-inspector`
  (react <=18) only make npm warn `ERESOLVE overriding peer dependency`.

Checked locally: 28 web tests, build, `./script/test`, `./script/lint`, and
the Go binary run with `--test` and the build embedded, driven in Chromium
(dashboard, `/board`, `/b/NHL`, API docs, nav, a switch saved to the config
file, Jump, cache headers).

### 3. Board pruning (waiting on the owner)

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

## Deliberately left alone

These are unmaintained but working, and are linked into the binary:
`golang/freetype` (2017), `nfnt/resize` (archived), `disintegration/imaging`,
`ghodss/yaml`, `gopkg.in/yaml.v2`/`v3` (archived), `robfig/cron/v3`.

None has a known issue that matters here. Replacing them means rewriting text
and logo rendering, or the config parsing. Config parsing in particular relies
on `ghodss/yaml`'s YAML 1.1 behaviour (a plain `NO` is `false`) and on
`internal/conffile`'s edit-and-verify. Revisit only with a reason.

## Small loose ends

- `README.md` shows a "Stock Ticker" screenshot for a board that doesn't
  exist, and `assets/images/tv_nhl_stats.jpg` isn't referenced anywhere.
- The XFL's successor, the UFL, has an ESPN feed (`football/ufl`) if the owner
  wants it. Adding it is roughly the XFL code #24 deleted.
- Nothing lints the web UI since #27: CRA ran its eslint config during the
  build, and it went with CRA. An eslint 10 flat config with
  `eslint-plugin-react-hooks` would bring it back.
- `web/src/matrix.swagger.json` is behind the protos: `SearchShows` and the
  newer `BoardSettings` fields are missing, and every Empty `$ref` points at a
  `<pkg>_google.protobuf.Empty` definition that doesn't exist. `./script/doc-gen`
  (needs `jq`) regenerates it; item 1 left it alone to stay small.
- v0.0.5's auto-generated release notes only list #24; #22 and #23 shipped in
  it too. Fix by hand on the release page if it matters.

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
  not run (swagger-ui's dependency); it did before item 1 too, and the tests
  and build don't need them.
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
- **Releases:** run `release.yml` via workflow_dispatch with `version`. A
  version containing `beta` is marked prerelease; anything else becomes
  "Latest".
- **On the Pi:** `sudo dpkg -i --force-confdef --force-confold
  sportsmatrix-*.deb`. The package is `sportsmatrix-<version>_aarch64.deb`,
  hyphen after the name.
