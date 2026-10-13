#!/bin/sh
# Data in /var/lib/dockler and the dockler user are kept; deb purge removes them.
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi
if [ "$1" = "purge" ]; then
	rm -rf /var/lib/dockler
	userdel dockler >/dev/null 2>&1 || true
fi
exit 0
