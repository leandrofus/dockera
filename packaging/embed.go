// Package packaging holds the files shipped by the rpm/deb packages; they are
// embedded so `ducktainer install-service` installs exactly the same setup.
package packaging

import "embed"

//go:embed systemd pam ducktainer.env scripts
var Files embed.FS
