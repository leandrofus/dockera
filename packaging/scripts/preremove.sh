#!/bin/sh
# deb: $1=remove|upgrade|deconfigure  ·  rpm: $1=0 (erase) | 1 (upgrade)
if [ "$1" = "remove" ] || [ "$1" = "0" ]; then
	if [ -d /run/systemd/system ]; then
		systemctl disable --now ducktainer.service ducktainer-pam.socket >/dev/null 2>&1 || true
	fi
fi
exit 0
