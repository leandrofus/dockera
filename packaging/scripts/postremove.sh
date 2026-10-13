#!/bin/sh
# Data in /var/lib/dockera and the dockera user are kept; deb purge removes them.
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi
if [ "$1" = "purge" ]; then
	rm -rf /var/lib/dockera
	userdel dockera >/dev/null 2>&1 || true
fi
exit 0
