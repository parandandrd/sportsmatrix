# sportsmatrix

Go software driving an RGB LED matrix sports scoreboard on a Raspberry Pi.
Fork of [robbydyer/sports](https://github.com/robbydyer/sports), diverged for
one person's build. See `## About this fork` in the README.

Target hardware: Pi 3 (Cortex-A53, 64-bit), Adafruit RGB Matrix HAT with the
GPIO 4<->18 anti-flicker mod, 64x32 panel, running Debian Trixie. 64-bit only.
The Pi 5 is not supported -- the vendored matrix library's model table stops at
`PI_MODEL_4` and a Pi 5 falls through to the Pi 3 branch and maps the wrong
peripheral base.

## Running the checks

```bash
./script/lint     # golangci-lint over the tree
./script/test     # builds the C library, then go test ./...
./script/build    # native if BUILDARCH matches the host, else a cross container
cd web && npm ci --legacy-peer-deps && npm test && npm run build
```

Things that will waste your time if you don't know them:

- **golangci-lint must be v1.64.6.** `.golangci.yml` is a v1 config and a v2
  binary refuses it outright with `unsupported version of the configuration`.
  That message is easy to skim past as a warning -- it means nothing was
  linted. The pinned version is `GOLANGCI_VERSION` in `script/common`.
- **`script/test` rebuilds the vendored C library** from
  `internal/rgbmatrix-rpi/lib/rpi-rgb-led-matrix.BASE` on every run, and
  removes it afterwards. First run takes a few minutes. Don't commit the
  non-`.BASE` copy.
- **`//go:embed assets`** in `internal/sportsmatrix/http.go` needs
  `internal/sportsmatrix/assets/web/` to exist with at least one file in it, or
  the package will not compile. `go:embed` ignores dotfiles, so a `.keep` does
  not work -- `script/test` and `script/build` drop a `placeholder` in.
- **`npm ci` needs `--legacy-peer-deps`.** swagger-ui-react declares a peer
  range of react `>=16.8.0 <19` while the project is on react 19.
- Tests in `internal/sportsmatrix` bind a real TCP port. Pick an unused one in
  any new test or you will get `address already in use` and a confusing hang.
- `SportsMatrix.Close()` sends on an unbuffered channel. Don't `defer s.Close()`
  in a test where `Serve` isn't running its normal loop -- it deadlocks.
- **The head of `/var/log/sportsmatrix_out.log` on the Pi can be an older
  run's.** The unit's `StandardOutput=file:` writes from the start of the file
  without truncating it, so a run that prints less leaves the last one's lines
  after its own.
- **Go's race detector won't start on a Raspberry Pi 5 kernel**
  (`ThreadSanitizer: unsupported VMA range`, from its 47-bit address space). A
  test for a concurrency fix has to fail without `-race` to prove anything there.

## Architecture worth knowing before you optimize

- The panel refresh runs on a **raw pthread at `SCHED_FIFO` priority 99 pinned
  to core 3** (`updater_->Start(99, (1<<3))` in the vendored `led-matrix.cc`),
  outside the Go runtime. Go's GC cannot stall the display. Allocation work is
  therefore a CPU and memory question, not a flicker question. `install.sh`
  adds `isolcpus=3` to the kernel command line so nothing else runs on that
  core; the Go runtime then sees three CPUs.
- One cgo call per frame: `led_matrix_swap`. The per-pixel loop is in C.
- A scroll **preloads every frame up front** into `Matrix.PreLoad`, then `Play`
  walks them on a timer. Frame buffers are reused across scrolls; `Play`
  truncates the list rather than discarding it. Don't reintroduce a per-frame
  allocation there -- it was 52% of allocation and 2.2GB per 2000 scrolls.
- `doBoard` renders **every canvas concurrently**, one goroutine each. There is
  one canvas now, the matrix. A board that shows several things in turn times
  each with `board.Hold(ctx, start, delay)` from when it began drawing it, so
  drawing time comes out of the display time rather than adding to it.
- **The web UI shows the panel's own frames.** `/api/panel/frame` is served by
  `matrix.Mirror`, which keeps a copy of each frame the matrix driver swaps in:
  exactly what the LEDs show, costing nothing extra. It answers with an ETag
  and 304, and `?wait=` holds the request until the frame changes. The
  dashboard preview and the `/board` page both use it, and both have a Full
  screen button (`web/src/fullscreen.js`), offered only where the browser
  allows it -- not iPhone Safari, which only does full screen for video.
- **There used to be a second, 800px canvas** (`internal/imgcanvas`) drawing
  every board again for a "full-res" web view. It was removed on 2026-09-18
  because the owner prefers the panel view; `git log --diff-filter=D --
  internal/imgcanvas` finds it. If something like it comes back: a second
  canvas means every board renders twice, concurrently -- that is where the
  "concurrent map writes" crash came from -- a board only draws to canvases
  that were on when it started, and it should only draw while someone is
  watching. The **web board launcher** went the same day: it started Chromium
  in kiosk mode on a display attached to the Pi (`launchWebBoard`, "Start web
  board", `/api/webboardon`), and the Pi runs Lite with no desktop, no browser
  and nothing on its HDMI port. `Status.webboard_on` is reserved in the proto.
- Boards implement `board.Board`, canvases implement `board.Canvas`. Board
  enable/disable goes through `board.Enabler`, whose `SetStateChangeCallback`
  wakes the serve loop when every board is off.
- RPC is Twirp over JSON, and the React app in `web/` is built and embedded
  into the binary. The web UI drives `ListBoards`, `SetBoardEnabled`,
  `SetBoardOrder` and `Jump`; the matrix settings `GetSettings`,
  `SetBrightness` and `SetScreenSchedule`; `GetBoardSettings` and
  `SetBoardSettings`; and each board's own service for its switches.
- Runtime config is `/etc/sportsmatrix.conf`, read with `ghodss/yaml` -- YAML
  1.1 rules, so a plain `NO` is `false`. The `.deb` ships one.
  `setConfigDefaults` builds **every board the binary knows**, whether the file
  has its section or not; `ListBoards` says which really are in the file.
- **Settings changed through the API are written back to that file** by
  `internal/conffile`, which edits the text in place so comments and blank
  lines survive, and refuses any edit whose result doesn't decode to exactly the
  old config plus the change. Board switches are saved by wrapping each board's
  RPC service where `startHTTP` mounts it (`savingHandler`), comparing status
  before and after. Hold `settingsLock` across changing a setting and saving it.
- The panel cycles through boards in the order of their **sections in the
  config file**, and `SetBoardOrder` moves the sections. The board list is
  replaced, never sorted in place: read it with `boardList()`.

## Conventions

- **History is linear.** PRs are rebase-merged. Don't add merge commits.
- **CI only runs on `pull_request`** (`.github/workflows/go.yml`). Pushing a
  branch on its own gets you no CI at all.
- **Each PR run also builds the arm64 `.deb`** on `ubuntu-24.04-arm` and keeps
  it as the run artifact `sportsmatrix-arm64-deb`. That is how to try a PR on
  the Pi before it merges, without a release: `gh run download <run-id> -n
  sportsmatrix-arm64-deb`, then `dpkg -i --force-confold`, which keeps the Pi's
  own `/etc/sportsmatrix.conf` instead of stopping to ask about it.
- **Releases are manual.** `release.yml` is `workflow_dispatch` with a version
  input (plus tag push). It runs on `ubuntu-24.04-arm` so the aarch64 build is
  native -- that took the build step from ~12 minutes under QEMU to ~17
  seconds. There is no scheduled workflow anywhere, and the repo owner has
  asked that there never be one.
- Work happens on a branch, not on master.

## State of things

Verified on the real Pi as of v0.0.3-beta.1: the dashboard, `ListBoards` /
`SetBoardEnabled`, NWSL, Go 1.23, the repackaged `.deb`.

Verified on the real Pi 3 on 2026-09-18, with the PR build
0.0.3-beta.1-19-g910404e, through the API: settings changed through it are
written to the config file one line at a time and survive a restart
(brightness, the screen schedule, a board switch, the board order, a display
time), bad values are refused without touching the file, and the file ends up
644 root. The web UI is served gzipped with the caching headers. The panel
frames endpoint follows the panel. `isolcpus=3` is set on it, and the refresh
thread has CPU 3 to itself. v0.0.4-beta.1 was installed on it with
`install.sh`, which found the Pi already set up.

Not verified on the Pi: anything only a person looking at it can see -- the
panel dimming, and the dashboard and `/board` in a browser, full screen
included.

Not verified end to end: the sport, stat and racing boards' live data paths.
ESPN's API is reachable from the Pi but was blocked from the sandbox these
changes were written in, so those paths have only ever been exercised against
fixtures.
