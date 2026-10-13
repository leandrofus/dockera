//go:build unix

package docker

import (
	"dockler/internal/i18n"
	"errors"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// permissionHint explains "permission denied" on a unix socket: who owns it
// and who we are, so the fix (`dockler setup`) is obvious.
func permissionHint(path string, err error) error {
	if !errors.Is(err, os.ErrPermission) {
		return err
	}
	st, serr := os.Stat(path)
	if serr != nil {
		return err
	}
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return err
	}
	name := func(f func(string) (string, error), id uint32) string {
		if n, e := f(strconv.Itoa(int(id))); e == nil {
			return n
		}
		return strconv.Itoa(int(id))
	}
	owner := name(func(id string) (string, error) {
		u, e := user.LookupId(id)
		if e != nil {
			return "", e
		}
		return u.Username, nil
	}, s.Uid)
	group := name(func(id string) (string, error) {
		g, e := user.LookupGroupId(id)
		if e != nil {
			return "", e
		}
		return g.Name, nil
	}, s.Gid)
	me := strconv.Itoa(os.Getuid())
	if u, e := user.Current(); e == nil {
		me = u.Username
	}
	return i18n.Errorf("no permission on %s (%s:%s %04o) for user %s; run `sudo dockler setup` to adapt the service to your Docker installation", path, owner, group, uint32(st.Mode().Perm()), me)
}
