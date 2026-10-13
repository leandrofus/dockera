#!/bin/sh
# deb: $1=configure  ·  rpm: $1=1 (install) | 2 (upgrade)
set -e

ensure_group() { getent group "$1" >/dev/null 2>&1 || groupadd -r "$1"; }
ensure_group dockler
ensure_group dockler-admins  # admins in the web UI (besides `docker`)
ensure_group dockler-viewers # read-only users

if ! getent passwd dockler >/dev/null 2>&1; then
	useradd -r -g dockler -d /var/lib/dockler -s /usr/sbin/nologin -c "dockler service" dockler
fi
install -d -o dockler -g dockler -m 0750 /var/lib/dockler
# data from a previous root-run install
chown -R dockler:dockler /var/lib/dockler 2>/dev/null || true

# How the service reaches Docker depends on how Docker is installed (socket
# group, rootless, root-only): detect it instead of assuming a docker group.
/usr/bin/dockler setup --no-restart --quiet || echo "warning: could not detect Docker; run \`sudo dockler setup\` once it is installed" >&2

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
	systemctl enable dockler-pam.socket dockler.service >/dev/null 2>&1 || true
	systemctl restart dockler-pam.socket >/dev/null 2>&1 || true
	systemctl restart dockler.service >/dev/null 2>&1 || true
fi

cat <<'MSG'

dockler is installed and listening on http://127.0.0.1:9900
  · Sign in with your system user if it belongs to `docker` or `dockler-admins`
    (read-only: `dockler-viewers`). E.g.: usermod -aG dockler-admins <user>
  · Detected Docker access:  sudo dockler setup --dry-run  (run `sudo dockler setup`
    again if you change the Docker installation)
  · Emergency token:         sudo dockler token
  · Open it to the network:  sudo dockler allow add 192.168.1.0/24 && sudo systemctl restart dockler
  · Options:                 /etc/dockler/dockler.env

MSG
