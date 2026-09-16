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
- **Go's race detector won't start on a Raspberry Pi 5 kernel**
  (`ThreadSanitizer: unsupported VMA range`, from its 47-bit address space). A
  test for a concurrency fix has to fail without `-race` to prove anything there.

## Architecture worth knowing before you optimize

- The panel refresh runs on a **raw pthread at `SCHED_FIFO` priority 99 pinned
  to core 3** (`updater_->Start(99, (1<<3))` in the vendored `led-matrix.cc`),
  outside the Go runtime. Go's GC cannot stall the display. Allocation work is
  therefore a CPU and memory question, not a flicker question.
- One cgo call per frame: `led_matrix_swap`. The per-pixel loop is in C.
- A scroll **preloads every frame up front** into `Matrix.PreLoad`, then `Play`
  walks them on a timer. Frame buffers are reused across scrolls; `Play`
  truncates the list rather than discarding it. Don't reintroduce a per-frame
  allocation there -- it was 52% of allocation and 2.2GB per 2000 scrolls.
- `doBoard` renders **every canvas concurrently**, one goroutine each. There are
  always at least two: the real matrix and an `imgcanvas` backing the browser
  `/board` view. Anything a board touches during `Render` needs to be safe for
  that.
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
- **Releases are manual.** `release.yml` is `workflow_dispatch` with a version
  input (plus tag push). It runs on `ubuntu-24.04-arm` so the aarch64 build is
  native -- that took the build step from ~12 minutes under QEMU to ~17
  seconds. There is no scheduled workflow anywhere, and the repo owner has
  asked that there never be one.
- Work happens on a branch, not on master.

## State of things

Verified on the real Pi as of v0.0.3-beta.1: the dashboard, `ListBoards` /
`SetBoardEnabled`, NWSL, Go 1.23, the repackaged `.deb`.

Not verified on the Pi yet, only against a local `run -t` instance started with
a copy of its config: everything written after v0.0.3-beta.1 -- the dashboard's
grouping, the web UI's caching, the web board launcher, saving settings to the
config file, reordering, brightness and the screen schedule, and board display
times and teams.

Not verified end to end: the sport, stat and racing boards' live data paths.
ESPN's API is reachable from the Pi but was blocked from the sandbox these
changes were written in, so those paths have only ever been exercised against
fixtures.
