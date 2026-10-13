package cloudflare

import (
	"os/exec"
	"syscall"
)

// cloudflared must not outlive ducktainer (a crash would leave the port
// published with nobody guarding it).
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
