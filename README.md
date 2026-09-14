# Raspberry Pi Sports LED Matrix

Go-based software to control a raspberry pi LED matrix.

![example1](assets/images/nhl_example2.jpg)

#### Table of Contents

- [Getting Help](#getting-help)<br>
- [Donations/Beer Money](#donations-and-beer-money)<br>
- [Board Types](#current-board-types)<br>
- [Installation](#installation)<br>
- [Configuration](#configuration)<br>
- [Running the Board](#running-the-board)<br>
- [Web UI Controller](#web-ui)<br>
- [API Endpoint](#api-endpoints)<br>
- [Contributing/Development](#contributing)<br>
- [Examples](#examples)<br>

## Getting Help

There's a public Discord channel, "RGB Sportsmatrix Help" <https://discord.gg/8vPp4xfdtV>

## Premium Version
This repo contains the FREE version of this app. The free version is no longer receiving new features- it will only get bug fixes and dependency updates. To get the latest and greatest features, check out the Premium version.

The new PREMIUM version of this app that adds the following features:
- **Scroll mode** - enable scrolling for the entire board or on a per-board basis
- **Weather Board**
- **Spotify Now-Playing**
- **MLB Live View** - shows baserunners, outs, pitch count, inning
- **Stock Ticker**
- **Gambling Odds** - shows gambling odds overlaid on each game's scoreboard (where available)
- **Additional Sports** 
  - FIFA Women's World Cup
  - More to Come!

Visit the [Patreon Page](https://patreon.com/RGBLEDMatrixTickerSoftware?utm_medium=clipboard_copy&utm_source=copyLink&utm_campaign=creatorshare_creator&utm_content=join_link) to get a membership.
Visit the [PREMIUM Version Installation Instructions](https://github.com/robbydyer/rgb-led-matrix-sports-premium)


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

This project currently supports all Raspberry Pi's with an armv7l or aarch64 architecture. This includes Pi 3b, 4, Zero 2. Pi's with the armv6 architecture are no longer supported,
but those can run version v0.0.83 and older- this would include the original Pi Zero.

You can check your Pi's architecture by running the following command:

```shell
dpkg --print-architecture
```

### Install script

There's a helper install script that pulls the latest release's .deb package and installs it and starts the service. Obviously, piping a
remote script to `sudo bash` is risky, so please take a look at `script/install.sh` to verify nothing nefarious is going on. You can always manually download the .deb package in the [Releases Section](https://github.com/robbydyer/sports/releases/latest). Just make sure to pick the correct one for your architecture.

Run the following command in a Terminal on your Pi

```shell
curl https://raw.githubusercontent.com/robbydyer/sports/master/script/install.sh | sudo bash
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
architecture, and enables the service so it survives a reboot:

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
workflow builds `.deb` packages for aarch64 and armv7l and attaches them to a
release. Download the one matching `dpkg --print-architecture` on your Pi.

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
