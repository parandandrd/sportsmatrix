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

### 1. Small cleanup PR (Go side)

**a. Drop the deprecated `github.com/golang/protobuf` from the binary.**
`proto/google/protobuf/empty.proto` is a repo-local copy of the well-known type
with `option go_package = "github.com/golang/protobuf/ptypes/empty";`. Every
generated `*.pb.go` and `*.twirp.go` in `internal/proto/` imports
`github.com/golang/protobuf/ptypes/empty` because of it, and that is the only
reason the deprecated module is linked in. Fix:

- Delete the local copy so protoc's bundled `google/protobuf/empty.proto` is
  used, or set its `go_package` to
  `google.golang.org/protobuf/types/known/emptypb`. Check which `-I` paths
  `script/proto-gen` passes before choosing.
- Regenerate with `./script/proto-gen`. It needs `protoc`, which the sandbox
  does not have: try `apt-get install -y protobuf-compiler`. The committed
  files say protoc 3.21.12 and protoc-gen-go v1.36.5. The vendored
  protoc-gen-go is now v1.36.12, so every header line will change; that is
  expected (see the comment in `script/proto-gen`).
- The old `ptypes/empty` package is a type alias for `emptypb.Empty`, so Go
  code calling these services should not change. Check `grep -rn 'empty\.'
  --include=*.go cmd internal` for direct uses.
- The web UI's `*_pb.js` stubs are only regenerated if `protoc-gen-js` is
  installed. The UI talks JSON over plain fetch, so stale JS stubs are fine.
- Then `go mod tidy && go mod vendor`; `github.com/golang/protobuf` should
  leave the `require` block.

**b. Trim `internal/tools/tools.go`.** It blank-imports seven codegen tools; the
scripts use three:

- used: `google.golang.org/protobuf/cmd/protoc-gen-go` and
  `github.com/twitchtv/twirp/protoc-gen-twirp` (`script/proto-gen`),
  `github.com/go-bridget/twirp-swagger-gen/cmd/twirp-swagger-gen`
  (`script/doc-gen`)
- unused: `github.com/pseudomuto/protoc-gen-doc`,
  `github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2`,
  `github.com/srikrsna/protoc-gen-gotag`,
  `github.com/thechriswalker/protoc-gen-twirp_js`

The unused four are what pull the deprecated `aws-sdk-go`, Google Cloud
libraries, gRPC and OpenTelemetry into `go.mod`. Remove them, `go mod tidy &&
go mod vendor`, and confirm `script/proto-gen` and `script/doc-gen` still find
what they build. Grep the repo for each name first.

**c. Delete `Dockerfile.protoc`.** Alpine 3.17 and Go 1.21.1, both end of life,
and nothing references it (`grep -rn Dockerfile.protoc`). `dockerbuild.yml`
only builds `Dockerfile.pibuilder`.

**d. `npm update` in `web/`** for the in-range bumps: react/react-dom 19.0 ->
19.3, bootstrap 5.3.3 -> 5.3.8, swagger-ui* 5.20 -> 5.33, postcss, ws, tar
and others (`npm outdated`). Use `npm install --legacy-peer-deps` semantics;
`npm ci` needs that flag (see `CLAUDE.md`). Run `npm test` and `npm run build`
on Node 24. Node 24 isn't in the sandbox by default; it downloads from
nodejs.org through the proxy, checked against `SHASUMS256.txt`.

Checks for the PR: `./script/lint`, `./script/test`, `BUILDARCH=x86_64
./script/build`, `cd web && npm test && npm run build`. Then try the PR's
arm64 `.deb` on the Pi.

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
- Shipped code that is behind: `react-router-dom` 6.21.3 (latest 7.x; check
  what `web/src` uses before choosing 6.30.x vs 7) and `swagger-ui-react`.
  `--legacy-peer-deps` is only there because swagger-ui-react declares a peer
  range of react `<19`; recheck whether 5.33 still does.
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
- v0.0.5's auto-generated release notes only list #24; #22 and #23 shipped in
  it too. Fix by hand on the release page if it matters.

## Things that will save time in the sandbox

- **Go:** `GOTOOLCHAIN=go1.27.1` downloads the toolchain through the module
  proxy.
- **golangci-lint:** `./script/install-golangci-lint <dir>` installs v2.14.0.
  The sandbox's preinstalled 2.5.0 is built with Go 1.25 and refuses this
  module.
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
