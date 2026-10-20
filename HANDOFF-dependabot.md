# Handoff: keeping sportsmatrix current (written 2026-10-20)

Scratch notes for the next session. Not part of the project: delete this file before the branch merges.

Scope is **sportsmatrix only**. The owner does not care about seeed-voicecard.

## Where things stand
Branch `claude/loving-ptolemy-0noibv`, two commits on top of master `d080049`, pushed, **no PR opened**:
- `78342c1` rewrote `.github/dependabot.yml`, added `script/check-updates`, one line in `CLAUDE.md`.
- `9ae400d` dropped the docker entry, fixed the actions group, recorded Dependabot as the owner's one exception to "no scheduled workflow".

`.github/dependabot.yml` now: gomod `/`, npm `/web`, github-actions `/`. Weekly (Monday). minor+patch grouped for gomod and npm; majors get their own PR. The actions group has no update-types filter on purpose (workflows pin `@v7`, so only majors ever show up). Validated against the SchemaStore dependabot-2.0 schema, 0 errors.

## Decisions the owner made
- Use Dependabot (a GitHub service, not a scheduled workflow). Weekly, grouped.
- Add a read-only `script/check-updates` for what Dependabot cannot see.
- Rebuilding the config from scratch was fine; do not keep things only because they were there.

## Still to do
1. **Owner:** repo Settings > Advanced Security: enable Dependency graph, Dependabot alerts, security updates and version updates. Dependabot has never opened a PR here and there is no "Dependabot Updates" workflow, so it is almost certainly off (inferred; repo settings are not readable from a session).
2. Open the PR when the owner says so. Rebase-merge; history is linear. CI only runs on `pull_request`.
3. After merge: Insights > Dependency graph > Dependabot > "Check for updates". Watch the first Go PR (does it update `vendor/`? does the updater cope with `go 1.27`?) and the first npm PR (swagger-ui peer warnings). Neither can be tested from here.
4. Delete this file.

## Behind as of 2026-10-20 (nothing applied yet)
- Go 1.27.1 -> 1.27.2 in `script/common` and `Dockerfile.pibuilder`; golangci-lint 2.14.0 -> 2.15.0. `script/update-go` moves them but uses `gsed`, so it only works on macOS. CI itself already takes the newest 1.27 patch.
- Go modules: `x/image` 0.46 -> 0.47, `x/sync` 0.23 -> 0.24 (pulls `x/sys` 0.49, `x/text` 0.43). Tried in a scratch copy: `go mod vendor` is consistent and the non-cgo tests pass.
- npm minor/patch (10 packages): react + react-dom 19.3.1, swagger-ui-react 5.34.0, eslint 10.13.0, jsdom 30.1.3, vite 8.3.4, vitest 5.0.5, user-event 14.6.8. `npm update` handles all of them.
- npm majors, owner's call: react-router 7.18.4 -> 8.5.0, @testing-library/jest-dom 6.9.1 -> 7.0.1.
- `npm audit`: 4 moderate, all via swagger-ui-react -> remarkable -> argparse. The only fix is a breaking `--force`; left alone.
- Vendored matrix library: pinned `51d3231` (2026-09-07), upstream `14464b3` (2026-10-10), 11 commits behind. All Python-binding, font-width and `make install` changes, nothing in the display driver. Low priority.
- Current: Node 24 (LTS 24.22.0), every GitHub Action, the rest of the Go modules.

## Optional follow-ups, only if the owner asks
- One PR applying the in-range updates above instead of waiting for Dependabot's first run.
- Cleanup PR for leftovers of removed features, none referenced by CI or another script: `script/gcal-setup.sh`, `script/render_mlb_test_data.sh`, `script/render_nhl_test_data.sh`, `script/mlb_ids.txt`, `script/nhl_ids.txt` (the last four write into `internal/mlb` and `internal/nhl`, which are gone), and README line 75 "Player Stats boards". Unreferenced but unclear, not touched: `download_stats`, `run_web_docker.sh`, `shell.console`, `test_img_jump.sh`, `test_rpc.sh`, `web-build`.

## Known gaps
- `go.yml` has a `paths` filter, so a Dependabot PR touching only `release.yml` or `dockerbuild.yml` gets no CI. A bump of `docker/*` or `ncipollo/release-action` alone is not exercised until the release or builder-image workflow runs.
- Nothing runs `Dockerfile.pibuilder` in CI or the release (they build natively), and its `debian:trixie-slim` tag has no version to bump. Hence no docker entry.

## Gotchas hit this session
- `set -o pipefail` plus `head` or `grep -m1` on the right of a pipe exits 141 (SIGPIPE). Use `git log -n 10`, a here-string, or `|| true`.
- `go list -m all` refuses `-mod=vendor`. To check vendoring use `go list -mod=vendor ./...`, after dropping a file into `internal/sportsmatrix/assets/web/` (the `go:embed` placeholder in `CLAUDE.md`).
- The sandbox's GitHub MCP only reaches `parandandrd/sportsmatrix` and `parandandrd/seeed-voicecard`. Upstreams were checked with `git ls-remote` and a shallow fetch instead. Reachable by curl: `proxy.golang.org`, `registry.npmjs.org`, `nodejs.org`. Not reachable: `go.dev` (DNS), and `api.github.com` returned 403 to WebFetch.
- The local `go` is 1.24; it downloads the 1.27 toolchain on demand, which worked.

## Not sportsmatrix, but loose
My `seeed-voicecard` branch `claude/loving-ptolemy-0noibv` is still on its remote (one commit, `check-upstream.sh`). `git push --delete` failed from the sandbox. The owner can delete it on GitHub's Branches page. Do not touch that repo further.
