//go:build windows

package compose

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// listenPrivate opens a named pipe that only the current user can open and
// returns its DOCKER_HOST.
func listenPrivate(string) (net.Listener, string, error) {
	tok := windows.GetCurrentProcessToken()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, "", err
	}
	b := make([]byte, 8)
	rand.Read(b)
	name := `\\.\pipe\ducktainer-` + hex.EncodeToString(b)
	ln, err := winio.ListenPipe(name, &winio.PipeConfig{
		// owner: full access; nobody else
		SecurityDescriptor: "D:P(A;;GA;;;" + u.User.Sid.String() + ")",
	})
	if err != nil {
		return nil, "", err
	}
	return ln, "npipe://" + strings.ReplaceAll(name, `\`, "/"), nil
}
