#!/bin/bash
#
# Installs sportsmatrix on a Raspberry Pi and performs the setup the LED matrix
# library needs, which a plain "dpkg -i" does not do:
#
#   - switches off the onboard sound, which uses the same PWM hardware as the
#     matrix. The library refuses to start while snd_bcm2835 is loaded.
#   - reserves CPU 3 for the panel refresh (isolcpus=3), which the library
#     asks for every time it starts
#   - sets the GPIO mapping for your board
#   - enables the service so it comes back after a reboot
#
# Run it with sudo. It is safe to run again; it only changes what is not
# already correct.
#
#   sudo ./install.sh                     # keep the current GPIO mapping
#                                         # (adafruit-hat-pwm on a new install)
#   sudo ./install.sh --adafruit-hat      # Adafruit RGB Matrix HAT/Bonnet
#   sudo ./install.sh --adafruit-hat-pwm  # ...with the GPIO 4-18 solder mod
#   sudo ./install.sh --regular           # directly wired, no HAT
#   sudo ./install.sh --tag v0.0.1-beta.2 # install one specific release
#
set -euo pipefail

REPO="${SPORTSMATRIX_REPO:-parandandrd/sportsmatrix}"
CONF="/etc/sportsmatrix.conf"
BLACKLIST="/etc/modprobe.d/blacklist-rgb-matrix.conf"

MAPPING=""
TAG=""
NEED_REBOOT=no

say() { printf '==> %s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --adafruit-hat)     MAPPING="adafruit-hat" ;;
    --adafruit-hat-pwm) MAPPING="adafruit-hat-pwm" ;;
    --regular)          MAPPING="regular" ;;
    --mapping)          shift; MAPPING="${1:-}" ;;
    --tag)              shift; TAG="${1:-}" ;;
    -h|--help)          sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)                  die "unknown option '$1' (try --help)" ;;
  esac
  shift
done

[ "$(id -u)" -eq 0 ] || die "run this with sudo"

for cmd in curl dpkg systemctl; do
  command -v "$cmd" >/dev/null || die "missing required command: $cmd"
done

# ---------------------------------------------------------------- architecture
case "$(dpkg --print-architecture)" in
  arm64)  ASSET_ARCH="aarch64" ;;
  armhf)  die "this needs 64-bit Raspberry Pi OS. Your Pi reports armhf (32-bit); reflash with the 64-bit image." ;;
  armel)  die "armv6 Pis are not supported. You need a Pi 3, 4, or Zero 2." ;;
  *)      die "unsupported architecture: $(dpkg --print-architecture)" ;;
esac
say "Architecture: ${ASSET_ARCH}"

# ----------------------------------------------------------------- the package
if [ -n "${TAG}" ]; then
  API="https://api.github.com/repos/${REPO}/releases/tags/${TAG}"
else
  API="https://api.github.com/repos/${REPO}/releases"
fi

say "Looking up the release on ${REPO}"
URL="$(curl -fsSL "${API}" \
  | grep -o '"browser_download_url"[^,]*' \
  | sed 's/.*": *"//; s/"$//' \
  | grep "_${ASSET_ARCH}\.deb$" \
  | head -1 || true)"

[ -n "${URL}" ] || die "no ${ASSET_ARCH} .deb found${TAG:+ for tag ${TAG}} on ${REPO}"

tmp="$(mktemp -d /tmp/sportsinstall.XXXXXX)"
trap 'rm -rf "${tmp}"' EXIT

say "Downloading $(basename "${URL}")"
curl -fsSL -o "${tmp}/sportsmatrix.deb" "${URL}"

say "Installing"
# --force-confold keeps an existing /etc/sportsmatrix.conf across upgrades.
dpkg -i --force-confdef --force-confold "${tmp}/sportsmatrix.deb"

# ------------------------------------------------------------- onboard sound
# The matrix drives its panel with the same PWM peripheral the Pi uses for
# analog audio, so the two cannot coexist.
if ! grep -qs '^blacklist snd_bcm2835' "${BLACKLIST}"; then
  say "Blacklisting the snd_bcm2835 sound module"
  echo "blacklist snd_bcm2835" >> "${BLACKLIST}"
  NEED_REBOOT=yes
  if command -v update-initramfs >/dev/null; then
    update-initramfs -u
  fi
fi

for f in /boot/firmware/config.txt /boot/config.txt; do
  [ -f "${f}" ] || continue
  if grep -q '^dtparam=audio=on' "${f}"; then
    say "Turning off onboard audio in ${f}"
    sed -i 's/^dtparam=audio=on/dtparam=audio=off/' "${f}"
    NEED_REBOOT=yes
  elif ! grep -q '^dtparam=audio=' "${f}"; then
    say "Turning off onboard audio in ${f}"
    echo 'dtparam=audio=off' >> "${f}"
    NEED_REBOOT=yes
  fi
  break
done

if lsmod 2>/dev/null | grep -q '^snd_bcm2835'; then
  NEED_REBOOT=yes
fi

# ------------------------------------------------------------- refresh core
# The library refreshes the panel from a realtime thread pinned to CPU 3.
# isolcpus=3 keeps everything else off that CPU, which steadies the refresh,
# and the library suggests it every time it starts until it is set. The line
# must stay one line: the firmware only reads the first. /boot/cmdline.txt is
# a "DO NOT EDIT" note on newer systems, hence the check for root=.
for f in /boot/firmware/cmdline.txt /boot/cmdline.txt; do
  [ -f "${f}" ] && grep -q 'root=' "${f}" || continue
  if ! grep -q 'isolcpus=' "${f}"; then
    say "Reserving CPU 3 for the panel refresh in ${f}"
    sed -i '1 s/$/ isolcpus=3/' "${f}"
    NEED_REBOOT=yes
  fi
  break
done

# ------------------------------------------------------------- GPIO mapping
if [ -n "${MAPPING}" ]; then
  if [ -f "${CONF}" ]; then
    if grep -q '^ *hardwareMapping:' "${CONF}"; then
      say "Setting hardwareMapping to ${MAPPING}"
      sed -i "s/^\( *hardwareMapping:\).*/\1 ${MAPPING}/" "${CONF}"
    else
      warn "no hardwareMapping line in ${CONF}; set it by hand under hardwareConfig"
    fi
  else
    warn "${CONF} not found, cannot set the GPIO mapping"
  fi
else
  current="$(grep -s '^ *hardwareMapping:' "${CONF}" | awk '{print $2}' || true)"
  say "Leaving the GPIO mapping alone (${current:-unknown})"
  if [ "${current}" = "adafruit-hat-pwm" ]; then
    say "  That needs GPIO 4 wired to GPIO 18 on the HAT or Bonnet."
    say "  No wire? Re-run with --adafruit-hat."
  else
    say "  Using an Adafruit HAT? Re-run with --adafruit-hat, or"
    say "  --adafruit-hat-pwm if you soldered GPIO 4 to GPIO 18."
  fi
fi

# ------------------------------------------------------------------- service
# enable, not just restart: the unit's [Install] section does nothing until
# enable creates the multi-user.target.wants symlink systemd reads at boot.
say "Enabling and starting the service"
systemctl enable sportsmatrix
systemctl restart sportsmatrix

echo
say "Installed. Useful commands:"
echo "    systemctl status sportsmatrix"
echo "    journalctl -u sportsmatrix -f"
echo "    sudo nano ${CONF}     # then: sudo systemctl restart sportsmatrix"

if [ "${NEED_REBOOT}" = "yes" ]; then
  echo
  warn "Reboot required to finish the setup: sudo reboot"
fi
