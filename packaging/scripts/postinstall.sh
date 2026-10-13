#!/bin/sh
# deb: $1=configure  ·  rpm: $1=1 (install) | 2 (upgrade)
set -e

ensure_group() { getent group "$1" >/dev/null 2>&1 || groupadd -r "$1"; }
ensure_group dockera
ensure_group dockera-admins  # admins in the web UI (besides `docker`)
ensure_group dockera-viewers # read-only users

if ! getent passwd dockera >/dev/null 2>&1; then
	useradd -r -g dockera -d /var/lib/dockera -s /usr/sbin/nologin -c "dockera service" dockera
fi
install -d -o dockera -g dockera -m 0750 /var/lib/dockera
# data from a previous root-run install
chown -R dockera:dockera /var/lib/dockera 2>/dev/null || true

# Key that encrypts the secrets in config.json. Kept outside the data
# directory (backups of /var/lib/dockera don't carry it), readable only by
# root; the service gets it through systemd LoadCredential.
install -d -m 0755 /etc/dockera
if [ ! -s /etc/dockera/secret.key ]; then
	(umask 077 && od -An -tx1 -N32 /dev/urandom | tr -d ' \n' > /etc/dockera/secret.key && echo >> /etc/dockera/secret.key)
	# a key created by the service before packaging moved it here
	if [ -s /var/lib/dockera/secret.key ]; then cp /var/lib/dockera/secret.key /etc/dockera/secret.key; fi
fi
chown root:root /etc/dockera/secret.key
chmod 0400 /etc/dockera/secret.key

# How the service reaches Docker depends on how Docker is installed (socket
# group, rootless, root-only): detect it instead of assuming a docker group.
/usr/bin/dockera setup --no-restart --quiet || echo "warning: could not detect Docker; run \`sudo dockera setup\` once it is installed" >&2

if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
	systemctl enable dockera-pam.socket dockera.service >/dev/null 2>&1 || true
	systemctl restart dockera-pam.socket >/dev/null 2>&1 || true
	systemctl restart dockera.service >/dev/null 2>&1 || true
fi

cat <<'MSG'

dockera is installed and listening on http://127.0.0.1:9900
  · Sign in with your system user if it belongs to `docker` or `dockera-admins`
    (read-only: `dockera-viewers`). E.g.: usermod -aG dockera-admins <user>
  · Detected Docker access:  sudo dockera setup --dry-run  (run `sudo dockera setup`
    again if you change the Docker installation)
  · Emergency token:         sudo dockera token --rotate  (only its hash is stored)
  · Open it to the network:  sudo dockera allow add 192.168.1.0/24 && sudo systemctl restart dockera
  · Options:                 /etc/dockera/dockera.env

MSG
