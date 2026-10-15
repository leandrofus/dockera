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

func dialRelay(context.Context, string) (net.Conn, bool, error) { return nil, false, nil }
