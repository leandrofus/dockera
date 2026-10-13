#!/bin/sh
# deb: $1=configure  ·  rpm: $1=1 (install) | 2 (upgrade)
set -e

ensure_group() { getent group "$1" >/dev/null 2>&1 || groupadd -r "$1"; }
ensure_group ducktainer
ensure_group ducktainer-admins  # admins in the web UI (besides `docker`)
ensure_group ducktainer-viewers # read-only users

if ! getent passwd ducktainer >/dev/null 2>&1; then
	useradd -r -g ducktainer -d /var/lib/ducktainer -s /usr/sbin/nologin -c "ducktainer service" ducktainer
fi
install -d -o ducktainer -g ducktainer -m 0750 /var/lib/ducktainer
# data from a previous root-run install
chown -R ducktainer:ducktainer /var/lib/ducktainer 2>/dev/null || true

# Key that encrypts the secrets in config.json. Kept outside the data
# directory (backups of /var/lib/ducktainer don't carry it), readable only by
# root; the service gets it through systemd LoadCredential.
install -d -m 0755 /etc/ducktainer
if [ ! -s /etc/ducktainer/secret.key ]; then
	(umask 077 && od -An -tx1 -N32 /dev/urandom | tr -d ' \n' > /etc/ducktainer/secret.key && echo >> /etc/ducktainer/secret.key)
	# a key created by the service before packaging moved it here
	if [ -s /var/lib/ducktainer/secret.key ]; then cp /var/lib/ducktainer/secret.key /etc/ducktainer/secret.key; fi
fi
chown root:root /etc/ducktainer/secret.key
chmod 0400 /etc/ducktainer/secret.key

# How the service reaches Docker depends on how Docker is installed (socket
# group, rootless, root-only): detect it instead of assuming a docker group.
/usr/bin/ducktainer setup --no-restart --quiet || echo "warning: could not detect Docker; run \`sudo ducktainer setup\` once it is installed" >&2

upgrade=
if [ "$1" = "2" ] || { [ "$1" = "configure" ] && [ -n "${2:-}" ]; }; then
	upgrade=1
fi

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
	if [ -n "$upgrade" ]; then
		# keep the admin's choice: only restart what is already running
		systemctl try-restart ducktainer-pam.socket ducktainer.service >/dev/null 2>&1 || true
	else
		systemctl enable ducktainer-pam.socket ducktainer.service >/dev/null 2>&1 || true
		systemctl restart ducktainer-pam.socket >/dev/null 2>&1 || true
		systemctl restart ducktainer.service >/dev/null 2>&1 || true
	fi
fi

[ -n "$upgrade" ] && exit 0

cat <<'MSG'

ducktainer is installed and listening on http://127.0.0.1:9900
  · Sign in with your system user if it belongs to `docker` or `ducktainer-admins`
    (read-only: `ducktainer-viewers`). E.g.: usermod -aG ducktainer-admins <user>
  · Detected Docker access:  sudo ducktainer setup --dry-run  (run `sudo ducktainer setup`
    again if you change the Docker installation)
  · Emergency token:         sudo ducktainer token --rotate  (only its hash is stored)
  · Open it to the network:  sudo ducktainer allow add 192.168.1.0/24 && sudo systemctl restart ducktainer
  · Options:                 /etc/ducktainer/ducktainer.env

MSG
