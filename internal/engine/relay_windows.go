//go:build windows

package engine

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"ducktainer/internal/docker"
	"ducktainer/internal/i18n"
)

// Relay keeps a WSL distribution running and serves its Docker daemon on
// docker.RelayPipe(distro) until ctx ends. The pipe is open to this user,
// LocalSystem (the ducktainer service) and the administrators. It runs at
// logon, from the task `engine install --mode wsl` creates.
func Relay(ctx context.Context, distro string) error {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sddl := fmt.Sprintf("D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;%s)", u.User.Sid.String())
	ln, err := winio.ListenPipe(docker.RelayPipe(distro), &winio.PipeConfig{SecurityDescriptor: sddl})
	if err != nil {
		return i18n.Errorf("could not open the relay of %s (is it already running?): %v", distro, err)
	}
	defer ln.Close()
	go keepRunning(ctx, distro)
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go relayConn(distro, c)
	}
}

// keepRunning holds a process in the distribution, so WSL doesn't stop it
// (and its containers) when nothing else uses it.
func keepRunning(ctx context.Context, distro string) {
	for ctx.Err() == nil {
		hidden(exec.CommandContext(ctx, "wsl.exe", "-d", distro, "-u", "root", "--", "sleep", "infinity")).Run()
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func relayConn(distro string, c net.Conn) {
	defer c.Close()
	cmd := hidden(exec.Command("wsl.exe", "-d", distro, "-u", "root", "--", "docker", "system", "dial-stdio"))
	cmd.Stdin, cmd.Stdout = c, c
	cmd.WaitDelay = time.Second // the client may keep its end open
	cmd.Run()
}

// Relays lists the distributions whose relay is running.
func Relays() []string {
	var d windows.Win32finddata
	h, err := windows.FindFirstFile(windows.StringToUTF16Ptr(`\\.\pipe\*`), &d)
	if err != nil {
		return nil
	}
	defer windows.FindClose(h)
	prefix := strings.TrimPrefix(docker.RelayPrefix, `\\.\pipe\`)
	var out []string
	for {
		if name := windows.UTF16ToString(d.FileName[:]); strings.HasPrefix(name, prefix) {
			out = append(out, strings.TrimPrefix(name, prefix))
		}
		if windows.FindNextFile(h, &d) != nil {
			return out
		}
	}
}
