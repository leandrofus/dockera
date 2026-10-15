//go:build windows

package docker

import (
	"context"
	"errors"
	"net"
	"os"

	"ducktainer/internal/i18n"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// RelayPrefix starts the name of the pipe through which `ducktainer engine
// relay` serves a WSL distribution's Docker daemon to other Windows accounts:
// WSL distributions belong to a user, and the ducktainer service runs as
// LocalSystem, which can't start them.
const RelayPrefix = `\\.\pipe\ducktainer-wsl-`

// RelayPipe is the relay pipe of a distribution.
func RelayPipe(distro string) string { return RelayPrefix + distro }

// TrustRelay decides whether to use a relay served by the account
// (DOMAIN\user, and its SID). By default only relays of this same user are
// used; the Windows service replaces it with its administrators policy, so
// another user can't pose as a Docker engine by creating the pipe first.
var TrustRelay = func(account, sid string) bool { return sid == currentSID() }

func currentSID() string {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return ""
	}
	return u.User.Sid.String()
}

// AsSystem reports whether this process runs as LocalSystem (the service).
func AsSystem() bool {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	return err == nil && u.User.Sid.IsWellKnown(windows.WinLocalSystemSid)
}

// dialRelay connects to the distribution's relay. ok is false when there is
// none and wsl.exe can be used instead.
func dialRelay(ctx context.Context, distro string) (c net.Conn, ok bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, pipeTimeout)
	defer cancel()
	c, err = winio.DialPipeContext(ctx, RelayPipe(distro))
	if err != nil {
		if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !os.IsNotExist(err) {
			return nil, true, err
		}
		if AsSystem() {
			return nil, true, i18n.Errorf("the WSL distribution %s is reached through its relay, which is not running: sign in to Windows as the user who installed it (the relay starts at logon) or run `ducktainer engine install --mode wsl` as that user", distro)
		}
		return nil, false, nil
	}
	account, sid, err := pipeServer(c)
	if err != nil {
		c.Close()
		return nil, true, err
	}
	if !TrustRelay(account, sid) {
		c.Close()
		return nil, true, i18n.Errorf("the relay of the WSL distribution %s runs as %s, who is not a ducktainer administrator", distro, account)
	}
	return c, true, nil
}

// pipeServer returns the account running the other end of a pipe.
func pipeServer(c net.Conn) (account, sid string, err error) {
	f, ok := c.(interface{ Fd() uintptr })
	if !ok {
		return "", "", errors.New("pipe without a handle")
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(f.Fd()), &pid); err != nil {
		return "", "", err
	}
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", "", err
	}
	defer windows.CloseHandle(p)
	var t windows.Token
	if err := windows.OpenProcessToken(p, windows.TOKEN_QUERY, &t); err != nil {
		return "", "", err
	}
	defer t.Close()
	u, err := t.GetTokenUser()
	if err != nil {
		return "", "", err
	}
	name, domain, _, err := u.User.Sid.LookupAccount("")
	if err != nil {
		return "", "", err
	}
	return domain + `\` + name, u.User.Sid.String(), nil
}
