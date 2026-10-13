package auth

import (
	"bufio"
	"ducktainer/internal/i18n"
	"encoding/json"
	"io"
	"net"
	"os"
	"time"
)

// HelperSocket is where the socket-activated root helper listens
// (ducktainer-pam.socket, root:ducktainer 0660). The web service itself runs
// unprivileged and only asks the helper "is this password right?".
const HelperSocket = "/run/ducktainer/pam.sock"

type helperReq struct {
	Service  string `json:"service"`
	User     string `json:"user"`
	Password string `json:"password"`
	Rhost    string `json:"rhost"`
}

type helperResp struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Login verifies a system user's password with whatever is available:
// PAM in-process when running as root, the root helper when running as the
// unprivileged service user, or in-process PAM as a last resort (works
// with pam_sss, or for the invoking user's own password).
func Login(service, user, password, rhost string) error {
	if os.Geteuid() != 0 {
		if _, err := os.Stat(HelperSocket); err == nil {
			return viaHelper(helperReq{service, user, password, rhost})
		}
	}
	if !Available {
		return ErrUnavailable
	}
	return Authenticate(service, user, password, rhost)
}

// Mode describes how Login will authenticate, for diagnostics.
func Mode() string {
	switch {
	case os.Geteuid() == 0 && Available:
		return "pam (in-process, root)"
	case fileExists(HelperSocket):
		return "pam via the root helper (" + HelperSocket + ")"
	case Available:
		return "unprivileged in-process pam (only SSSD/LDAP or your own user)"
	}
	return "not available"
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func viaHelper(req helperReq) error {
	conn, err := net.DialTimeout("unix", HelperSocket, 5*time.Second)
	if err != nil {
		return i18n.Errorf("could not reach the PAM helper: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(40 * time.Second))
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	var resp helperResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return i18n.Errorf("invalid response from the PAM helper")
	}
	if !resp.OK {
		if resp.Error == ErrBadCredentials.Error() {
			return ErrBadCredentials
		}
		return &i18n.Error{Msg: resp.Error} // translated if it's a known message
	}
	return nil
}

// ServeHelper handles one request on rw (stdin/stdout under systemd socket
// activation with Accept=yes). The PAM service is fixed by the helper's
// own command line, never taken from the (less trusted) client, so a
// caller can't pick a permissive stack. Failures are delayed.
func ServeHelper(service string, r io.Reader, w io.Writer) error {
	var req helperReq
	if err := json.NewDecoder(bufio.NewReader(io.LimitReader(r, 64<<10))).Decode(&req); err != nil {
		return json.NewEncoder(w).Encode(helperResp{Error: "invalid request"})
	}
	err := Authenticate(service, req.User, req.Password, req.Rhost)
	if err != nil {
		time.Sleep(time.Second)
		return json.NewEncoder(w).Encode(helperResp{Error: err.Error()})
	}
	return json.NewEncoder(w).Encode(helperResp{OK: true})
}
