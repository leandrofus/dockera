//go:build !windows

package compose

import (
	"net"
	"path/filepath"
)

// listenPrivate opens a socket only the current user can reach (it lives in
// the private temporary directory dir) and returns its DOCKER_HOST.
func listenPrivate(dir string) (net.Listener, string, error) {
	p := filepath.Join(dir, "docker.sock")
	ln, err := net.Listen("unix", p)
	if err != nil {
		return nil, "", err
	}
	return ln, "unix://" + p, nil
}
