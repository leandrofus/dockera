//go:build !linux

package cloudflare

import "os/exec"

func detach(cmd *exec.Cmd) {}
