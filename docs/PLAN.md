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

On the Pi: the owner installed the #22 and v0.0.4-beta.11 builds and the panel
looked fine. v0.0.5 and v0.0.6 have not been reported on.

Current as of v0.0.6: Go 1.27.1, golangci-lint 2.14.0, every Go module that is
linked into the binary at its latest minor/patch, CI actions at their latest
majors, Node 24, the matrix library at hzeller HEAD.

A correction to #25's description: `golang.org/x/net` and `x/crypto` are not
linked into the binary (`go list -deps ./cmd/sportsmatrix` doesn't list them).
The binary's HTTP and TLS are the standard library's, so those fixes came with
the Go 1.27 bump in #22.

## Work, in order

### 1. Small cleanup PR -- written, no PR yet

Done on branch `claude/nice-brown-bi6dbx` (four commits on `3881d45`), pushed
on 2026-10-05. **Left to do:** open the PR (CI only runs on `pull_request`),
get it green, have the owner try its arm64 `.deb` on the Pi, rebase-merge.

What it does:

- **a.** Deletes `proto/google/protobuf/empty.proto`, so protoc's own copy
  (`go_package` `.../types/known/emptypb`) is used, and regenerates
  `internal/proto/` with protoc 3.21.12 and protoc-gen-go v1.36.12. No
  hand-written Go code used `ptypes/empty`. `go version -m` on the built binary
  lists only `google.golang.org/protobuf`. `script/proto-gen`'s
  missing-protoc message now says a release-zip protoc needs its `include/`.
- **b.** `internal/tools/tools.go` keeps only the three tools the scripts use.
  `go mod tidy && go mod vendor` dropped `golang/protobuf`, grpc-gateway,
  gRPC, genproto, gogo/protobuf and more from `go.mod`, and ~127k lines from
  `vendor/`. `script/doc-gen` and `script/proto-gen` both still run.
- **c.** Deletes `Dockerfile.protoc`.
- **d.** `npm update --legacy-peer-deps` on Node 24.21.0: react/react-dom 19.3,
  bootstrap 5.3.8, swagger-ui* 5.33.1, postcss, ws, tar and the transitive
  tree. npm 11 rewrote the lockfile from version 2 to 3, which is most of its
  diff. `react-router-dom` was `>=6.0.0`, which `npm update` takes to 7; it is
  now `^6.21.3` and resolves to 6.30.6. The unused `>=` pins
  `hosted-git-info`, `is-svg` and `normalize-url` crossed a major (10.1.1,
  6.1.0, 9.0.1); nothing imports them.
- The `--legacy-peer-deps` explanation in `go.yml`, `release.yml` and
  `CLAUDE.md` is corrected (see item 2).

Checked locally: `./script/lint` 0 issues, `./script/test` passes,
`BUILDARCH=x86_64 ./script/build` builds, `npm test` (23 tests) and
`npm run build`, also with `CI=true`, pass on Node 24.

### 2. Move the web UI off Create React App (its own PR)

`npm audit` reports 88 findings (7 critical, 39 high). Nearly all are in
`react-scripts` 5.0.1's build tooling (babel, webpack-dev-server, express,
svgo, rollup), which never reaches the Pi. CRA is abandoned. Target: Vite.

Things the migration has to carry:

- `web/package.json` scripts are `react-scripts start|build|test`; tests run
  under Jest via react-scripts. Vitest is the natural replacement (7 test
  files, 23 tests, using `@testing-library/*`).
- Output directory: the build lands in `web/build/`, and `script/build`
  (lines ~31-32) and `script/web-build` (line ~38) copy `web/build` into
  `internal/sportsmatrix/assets/web`. Either set Vite's `build.outDir` to
  `build` or change both scripts.
- `web/public/index.html` uses CRA's `%PUBLIC_URL%`; Vite wants `index.html` at
  the project root with a module script entry.
- No `REACT_APP_*` env vars and no `proxy` field were found, so those don't
  need carrying.
- The `eslintConfig` extends `react-app`; replace or drop it.
- `package.json` has about 20 `>=` pins on transitive packages (`tar`,
  `lodash`, `elliptic`, `node-forge`, `nth-check`, `glob-parent`, `url-parse`,
  `set-value`, `tmpl`, `path-parse`, `json-schema`, `is-svg`, `dns-packet`,
  `ansi-regex`, `hosted-git-info`, `normalize-url`, `node-fetch`,
  `react-dev-utils`, `browserslist`, `follow-redirects` …). They were an old
  attempt to silence audit warnings under CRA. Most should simply go; check
  each with `grep -rn "from '<pkg>'" web/src` first.
- `react-router-dom` is `^6.21.3` (6.30.6) after item 1; latest is 7.x.
  `web/src` uses `BrowserRouter`, `Routes`, `Route`, `Navigate`, `Link`,
  `useParams` and `useLocation` (`App.js`, `Nav.js`, `BoardPage.js`).
- `--legacy-peer-deps` is still needed after item 1. swagger-ui-react 5.33
  allows react `<20`, but `react-debounce-input` 3.3.0 and `react-inspector`
  6.0.2, both pulled in by swagger-ui, stop at react 18. A strict `npm ci`
  also wants peers the lockfile never resolved (`@testing-library/dom`,
  `typescript`). Dropping the flag means fixing both and regenerating the
  lockfile without it.
- The served UI is gzipped with caching headers by the Go side; check that
  Vite's hashed asset names still get the long cache headers (see
  `internal/sportsmatrix/http.go`).
- In the browser, check the dashboard and `/board` (panel preview, Full screen
  button), and the swagger page.

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
