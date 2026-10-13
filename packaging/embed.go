// Package packaging holds the files shipped by the rpm/deb packages; they are
// embedded so `dockera install-service` installs exactly the same setup.
package packaging

import "embed"

//go:embed systemd pam dockera.env scripts
var Files embed.FS
