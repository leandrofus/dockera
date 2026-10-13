//go:build linux && cgo && pam

package auth

import (
	"ducktainer/internal/i18n"
	"errors"
	"sync"

	"github.com/msteinert/pam/v2"
)

// Available reports whether system-user authentication was compiled in.
const Available = true

// some PAM modules aren't thread-safe; logins are rare, serialize them
var pamMu sync.Mutex

// Authenticate checks a password through PAM (auth + account stacks), so
// shadow, SSSD/LDAP, lockouts and expiry policies all apply.
func Authenticate(service, username, password, remoteHost string) error {
	if username == "" || password == "" {
		return ErrBadCredentials
	}
	pamMu.Lock()
	defer pamMu.Unlock()
	t, err := pam.StartFunc(service, username, func(s pam.Style, msg string) (string, error) {
		switch s {
		case pam.PromptEchoOff:
			return password, nil
		case pam.PromptEchoOn:
			return username, nil
		case pam.ErrorMsg, pam.TextInfo:
			return "", nil
		}
		return "", errors.New("unsupported PAM conversation")
	})
	if err != nil {
		return err
	}
	defer t.End()
	if remoteHost != "" {
		_ = t.SetItem(pam.Rhost, remoteHost)
	}
	if err := t.Authenticate(pam.DisallowNullAuthtok); err != nil {
		return ErrBadCredentials
	}
	if err := t.AcctMgmt(pam.DisallowNullAuthtok); err != nil {
		return i18n.Errorf("account not valid: %v", err)
	}
	return nil
}
