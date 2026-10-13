//go:build windows

package docker

import (
	"context"
	"net"
	"os/exec"
	"syscall"

	"github.com/Microsoft/go-winio"
)

func dialPipe(ctx context.Context, path string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, pipeTimeout)
	defer cancel()
	return winio.DialPipeContext(ctx, path)
}

// hideWindow keeps helper processes (wsl.exe) from flashing a console.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
