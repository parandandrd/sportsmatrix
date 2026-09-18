# Raspberry Pi Sports LED Matrix

Go-based software to control a raspberry pi LED matrix.

![example1](assets/images/nhl_example2.jpg)

#### Table of Contents

- [Getting Help](#getting-help)<br>
- [About this fork](#about-this-fork)<br>
- [Board Types](#current-board-types)<br>
- [Installation](#installation)<br>
- [Configuration](#configuration)<br>
- [Running the Board](#running-the-board)<br>
- [Web UI Controller](#web-ui)<br>
- [API Endpoint](#api-endpoints)<br>
- [Contributing/Development](#contributing)<br>
- [Examples](#examples)<br>

## Getting Help

The upstream project runs a public Discord channel, "RGB Sportsmatrix Help"
<https://discord.gg/8vPp4xfdtV>. It is a good place for questions about the
hardware and the matrix library. Please do not take issues with *this* fork's
changes there -- open them here instead.

## About this fork

The original -- and the one you probably want -- is
[robbydyer/sports](https://github.com/robbydyer/sports) by Rob Dyer. Essentially
all of this code is his, it is an ongoing project, and he also develops a
premium edition with features this one does not have. If you want a supported
LED scoreboard, start there.

This is a personal fork, modified from that project since September 2026. It
exists to run one scoreboard in one house and is changed to suit that. It is not
a distribution, there is no roadmap, and nothing here is promised to anyone. Any
bugs you find are far more likely to be mine than Rob's.

It is [GPL v3](LICENSE), the same licence it was given upstream, and it stays
that way.

The panel itself is driven by [hzeller/rpi-rgb-led-matrix](https://github.com/hzeller/rpi-rgb-led-matrix)
(GPL v2), vendored under `internal/rgbmatrix-rpi/lib/`. That library does all
the real-time work -- the software PWM and GPIO timing that actually lights the
LEDs -- and none of this would exist without it.

Sports data comes from ESPN's public endpoints. This project is not affiliated
with, endorsed by, or supported by ESPN or any league.


## Current Board Types

- Sports. Shows upcoming, live, and completed games for the day (or the week for football), as well as news headlines:
  - NHL
  - MLB
  - NFL
  - MLS
  - NCAA Football
  - NCAAM Basketball
  - NCAAW Basketball
  - NBA
  - PGA Tour
  - WNBA
  - XFL
  - Soccer Leagues:
    - Spanish Laliga
    - FIFA World Cup
    - English Premier League
    - Italian Serie A
    - French Ligue 1
    - DFB Pokal
    - Bundesliga
- Racing. Currently just shows upcoming event schedule
  - F1
  - Indy Car
- Google Calendar
- Player Stats boards- currently supports MLB and NHL.
- Image Board: Takes a list of directories containg images and displays them. Works with GIF's too!
- Clock
- Sys: Displays basic system info. Currently Mem and CPU usage

## Installation

### Supported Pi

64-bit (`arm64`) only: a Pi 3, 4, or Zero 2 W running the 64-bit Raspberry Pi OS.

The Pi 5 is not supported -- the matrix library does not drive its GPIO.

Check what your Pi is running:

```shell
dpkg --print-architecture
```

### Raspberry Pi setup

The matrix library needs two things on the Pi that installing the package does
not do by itself, so `dpkg -i` alone will usually leave you with a dark panel:

- **The onboard sound has to be off.** The Pi drives analog audio with the same
  PWM peripheral the matrix uses, and the library refuses to start while the
  `snd_bcm2835` module is loaded.
- **The GPIO mapping has to match your wiring.** An Adafruit RGB Matrix
  HAT/Bonnet uses `adafruit-hat`, or `adafruit-hat-pwm` if you have soldered the
  anti-flicker wire between GPIO 4 and GPIO 18. Directly wired panels use
  `regular`.

`script/install.sh` does both, installs the latest release for your
architecture, and enables the service so it survives a reboot. It also adds
`isolcpus=3` to the kernel command line, reserving CPU 3 for the thread that
refreshes the panel -- the library suggests it every time it starts:

```shell
git clone https://github.com/parandandrd/sportsmatrix
sudo ./sportsmatrix/script/install.sh --adafruit-hat
```

Or, without cloning, against whichever repo you want it to pull releases from:

```shell
curl -fsSL https://raw.githubusercontent.com/parandandrd/sportsmatrix/master/script/install.sh \
  | sudo bash -s -- --adafruit-hat
```

Pass `--adafruit-hat-pwm`, `--regular`, or `--mapping <name>` to suit your
board, or no flag at all to leave the mapping alone. It is safe to re-run; it
only changes what is not already correct. Reboot afterwards if it tells you to.

Piping a remote script into `sudo bash` is worth being suspicious of, so read
[`script/install.sh`](script/install.sh) first. You can also skip it and take
the `.deb` straight from the
[releases page](https://github.com/parandandrd/sportsmatrix/releases/latest).

## Building your own

The install script above pulls a prebuilt release. If you've changed the code,
you need to build it yourself. Two ways:

**On the Pi itself.** Slowest to compile but needs nothing but the Pi:

```shell
git clone <your fork> sportsmatrix && cd sportsmatrix
./script/build.local
```

That produces `sportsmatrix.bin`. To include the web UI, run
`npm ci && npm run build` in `web/` first (needs the node version in `.nvmrc`,
which is happier on a real computer than on a Pi); without it the board and API
work fine and only the browser frontend is missing.

**With GitHub Actions.** Push a tag matching `v*` to your fork and the release
workflow builds an aarch64 `.deb` and attaches it to a release.

### Installing your build

If you built a `.deb`:

```shell
sudo dpkg -i sportsmatrix-*.deb
```

If you built a bare binary, drop it over the installed one:

```shell
sudo systemctl stop sportsmatrix
sudo cp sportsmatrix.bin /usr/local/bin/sportsmatrix
sudo systemctl start sportsmatrix
```

Your config at `/etc/sportsmatrix.conf` is preserved across `.deb` upgrades.
Logs are at `/var/log/sportsmatrix.log`, or `journalctl -u sportsmatrix -f`.

## Configuration

You can run the app without passing any configuration, it will just use some sane defaults. Currently it only defaults to showing the NHL board. Each board that is enabled will be rotated through. The default location for the config file is `/etc/sportsmatrix.conf`

See the [Full Example Configuration](sportsmatrix.conf.example)<br>

For a list of all possible team abbreviations (including conference/divisions when available), see [this list](TEAM_ABBREVIATIONS)<br>

## Running the Board

If you installed the app with the installer script or a .deb package directly, then the service will run automatically. You can start/stop/restart the service with systemctl commands:

```shell
# stops the service
sudo systemctl stop sportsmatrix

# Restarts the service, like after changes to the config file
sudo systemctl restart sportsmatrix
```

You can also run the app manually in the foreground. The .deb package installs the binary to `/usr/local/bin/sportsmatrix`
NOTE: You *MUST* run the app via sudo. The underlying C library requires it. It does switch to a less-privileged user after the matrix is initialized.

```shell
# Show all CLI options
sportsmatrix --help

# Run with defaults
sudo sportsmatrix.bin run

# With config file
sudo sportsmatrix.bin run -c myconfig.conf

# NHL demo mode
sudo sportsmatrix.bin nhltest
```

## Web UI

There is a (very) basic web UI frontend for managing the board. It is bundled with the binary and served as a single-page app. The UI gives buttons for all the backend [API Endpoints](#api-endpoints). You can also view a rendered version of the board in the "Board" section (make sure your configuration enables this). Front-end dev is not my strongsuit, so it's not particularly pretty.

The Web UI is accessible at `http://[HOSTNAME OR IP]:[PORT]`, where port is whatever you configure the `httpListenPort` in your config file to be. For example, if your Pi's hostname is `mypi` and your configured listen port is `8080`, `http://mypi:8080`

![example1](assets/images/ui4.png) ![example2](assets/images/ui3.png) ![example3](assets/images/ui2.png) ![example4](assets/images/ui1.png)

Example of the Web Board:<br>
![webboard](assets/images/tv_nhl.jpg)

## API endpoints

The Web UI has a built-in doc page describing the API. It also includes an interactive way to test API calls. There's a
button in the nav "API Docs", or you can go to `http://[YOURIP]/docs`

### Special "Jump only" Image directories

If you would like to configure certain image directories to contain "jump only" images (only seen when an API call is made to show them), you can
do so by configuring them like:

```
imageConfig:
  directoryList:
  - directory: /my/image/dir
    jumpOnly: true
```

Then, to display a particular image in that directory, make the API call and pass the desired image name.

```
curl -X POST --header "Content-Type: application/json" -d '{"name":"goal.gif"}' "http://myhost:myport/imageboard.v1.ImageBoard/Jump"
```

## Examples

NHL
![NHL example 2](assets/images/nhl_example.jpg)

MLB
![MLB example](assets/images/mlb_board.jpg)

STOCK TICKER
![Stock Ticker](assets/images/stock_ticker.jpg)

PGA Tour Leaderboard
![PGA Board](assets/images/pga.jpeg)

NHL Stats
![NHL Stats](assets/images/nhl_stats.jpg)

MLB Stats
![MLB Stats](assets/images/mlb_stats.jpg)

In real life, this is a GIF of Mario running. This is using the Image Board.
![image example](assets/images/mario_board.jpg)
