#!/bin/bash
set -euo pipefail

ROOT="$(dirname $( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd ))"
BUILDARCH="${BUILDARCH:-}"

cd "${ROOT}"

tmp="$(mktemp -d /tmp/sportsbuild.XXXX)"
echo "Build Dir: ${tmp}"

d="sportsmatrix-${VERSION}_${BUILDARCH}"

mkdir "${tmp}/${d}"
cd "${tmp}/${d}"

mkdir -p DEBIAN etc/systemd/system usr/local/bin etc/logrotate.d

cp "${ROOT}/sportsmatrix.${BUILDARCH}" usr/local/bin/sportsmatrix
chmod 755 usr/local/bin/sportsmatrix

# Architecture was "all", which claims the package runs anywhere. It is an
# arch-specific binary, so dpkg would happily install the arm64 build on a
# 32-bit Pi and leave it to fail at exec time.
case "${BUILDARCH}" in
  aarch64|arm64) DEB_ARCH=arm64 ;;
  armv7l|armhf)  DEB_ARCH=armhf ;;
  x86_64|amd64)  DEB_ARCH=amd64 ;;
  *)             DEB_ARCH=all ;;
esac

# The package declared no dependencies at all, so a Pi with an older glibc than
# the builder installed it cleanly and then died with "GLIBC_x.yz not found" in
# the journal, with nothing anywhere saying why. Read the floor off the binary
# rather than assuming the build host's version: it is the highest versioned
# symbol actually referenced, which is normally lower and therefore lets the
# package install on more systems, not fewer.
LIBC_MIN="$(objdump -T usr/local/bin/sportsmatrix 2>/dev/null \
  | grep -o 'GLIBC_[0-9]\+\.[0-9]\+\(\.[0-9]\+\)\?' \
  | sed 's/GLIBC_//' \
  | sort -uV \
  | tail -1 || true)"

{
  echo "Package: sportsmatrix"
  echo "Version: ${VERSION}"
  echo "Section: custom"
  echo "Priority: optional"
  echo "Architecture: ${DEB_ARCH}"
  echo "Essential: no"
  if [ -n "${LIBC_MIN}" ]; then
    echo "Depends: libc6 (>= ${LIBC_MIN})"
  fi
  echo "Maintainer: https://github.com/parandandrd/sportsmatrix"
  echo "Description: Live sports driver for RGB LED matrix"
} > DEBIAN/control

echo "=> ${DEB_ARCH} package, libc6 floor: ${LIBC_MIN:-not determined}"

cat <<EOF > etc/systemd/system/sportsmatrix.service
[Unit]
Description=Sportsmatrix
After=network.target
StartLimitIntervalSec=0

[Service]
Type=simple
Restart=always
RestartSec=1
User=root
ExecStart=/usr/local/bin/sportsmatrix run -f /var/log/sportsmatrix.log
StandardOutput=file:/var/log/sportsmatrix_out.log
StandardError=file:/var/log/sportsmatrix_out.log

[Install]
WantedBy=multi-user.target
EOF

cat <<EOF > DEBIAN/conffiles
/etc/sportsmatrix.conf
EOF

cat <<EOF > etc/logrotate.d/sportsmatrix
/var/log/sportsmatrix.log
{
        rotate 3
        daily
        missingok
        notifempty
        delaycompress
        compress
}

/var/log/sportsmatrix_out.log
{
        rotate 3
        daily
        missingok
        notifempty
        delaycompress
        compress
}
EOF

cat <<EOF > DEBIAN/postinst
sudo systemctl daemon-reload
sudo rm -rf /tmp/sportsmatrix*
# The unit's [Install] section only takes effect once enable has created the
# multi-user.target.wants symlink. Without this the service runs after install
# but does not come back after a reboot.
sudo systemctl enable sportsmatrix
sudo systemctl restart sportsmatrix
EOF

chmod 755 DEBIAN/postinst

cp "${ROOT}/sportsmatrix.conf.example" etc/sportsmatrix.conf

# This was 666, owned by whatever user ran the build, which on the release
# runner is uid 1001: any local user could rewrite the config of a service
# that runs as root. The service now writes the file itself when settings are
# changed through the web UI, and drops group and world write when it does.
chmod 644 etc/sportsmatrix.conf

cd "${tmp}"
dpkg-deb --root-owner-group --build "${d}"

mv "${d}.deb" "${ROOT}/"

cd "${ROOT}"
rm -rf "${tmp}"
