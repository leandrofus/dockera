//go:build !windows

package docker

import (
	"context"
	"net"
	"os/exec"

	"ducktainer/internal/i18n"
)

func dialPipe(context.Context, string) (net.Conn, error) {
	return nil, i18n.Errorf("named pipes (npipe://) only exist on Windows")
}

func hideWindow(*exec.Cmd) {}
