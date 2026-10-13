//go:build !(linux && cgo && pam)

package auth

// Available reports whether system-user authentication was compiled in.
// Build with `-tags pam` (and CGO, libpam headers) to enable it.
const Available = false

func Authenticate(service, username, password, remoteHost string) error {
	return ErrUnavailable
}
