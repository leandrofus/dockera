#!/bin/sh
# Data in /var/lib/ducktainer and the ducktainer user are kept; deb purge removes them.
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload >/dev/null 2>&1 || true
fi
# start menu entries (deb: remove|purge · rpm: 0 = erase, not upgrade)
if [ "$1" = "remove" ] || [ "$1" = "purge" ] || [ "$1" = "0" ]; then
	# the per-user watcher that refreshes the menu (installed by setup)
	systemctl --global disable ducktainer-menu.path >/dev/null 2>&1 || true
	rm -f /etc/systemd/user/ducktainer-menu.path /etc/systemd/user/ducktainer-menu.service
	rm -rf /usr/local/share/applications/ducktainer-containers
	rm -f /etc/xdg/menus/applications-merged/ducktainer-containers.menu \
		/usr/local/share/desktop-directories/ducktainer-containers.directory \
		/usr/local/share/icons/hicolor/scalable/apps/ducktainer.svg
fi
if [ "$1" = "purge" ]; then
	rm -rf /var/lib/ducktainer
	userdel ducktainer >/dev/null 2>&1 || true
fi
exit 0
