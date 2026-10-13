// Package auth authenticates users against the operating system (PAM on
// Linux) and maps them to dockler roles through POSIX groups.
package auth

import (
	"dockler/internal/i18n"
	"os/user"
	"slices"
	"sync"
	"time"
)

const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

var (
	ErrBadCredentials = i18n.Errorf("wrong username or password")
	ErrUnavailable    = i18n.Errorf("system-user authentication is not available in this build")
)

// Policy decides which system users may log in and with which role.
type Policy struct {
	AdminGroups  []string
	ViewerGroups []string
	AdminUsers   []string
	ViewerUsers  []string
	AllowRoot    bool
}

// Groups returns the names of every group the user belongs to (primary
// included), resolved through NSS so LDAP/SSSD users work too.
func Groups(username string) ([]string, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}
	ids, err := u.GroupIds()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if g, err := user.LookupGroupId(id); err == nil {
			names = append(names, g.Name)
		}
	}
	return names, nil
}

// Role resolves the role of a user, or an error if the user is not allowed.
func (p Policy) Role(username string) (string, error) {
	if username == "root" && !p.AllowRoot {
		return "", i18n.Errorf("root login is disabled (allow_root)")
	}
	if slices.Contains(p.AdminUsers, username) {
		return RoleAdmin, nil
	}
	groups, err := Groups(username)
	if err != nil {
		return "", i18n.Errorf("could not resolve the groups of %s: %v", username, err)
	}
	for _, g := range groups {
		if slices.Contains(p.AdminGroups, g) {
			return RoleAdmin, nil
		}
	}
	if slices.Contains(p.ViewerUsers, username) {
		return RoleViewer, nil
	}
	for _, g := range groups {
		if slices.Contains(p.ViewerGroups, g) {
			return RoleViewer, nil
		}
	}
	return "", i18n.Errorf("%s is not a member of any authorized group", username)
}

// Limiter slows down password guessing: after max failures for the same
// key within window, further attempts are refused until it expires.
type Limiter struct {
	mu     sync.Mutex
	fails  map[string][]time.Time
	max    int
	window time.Duration
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{fails: map[string][]time.Time{}, max: max, window: window}
}

// Blocked reports whether key is locked out and for how long.
func (l *Limiter) Blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.prune(key)
	if len(recent) < l.max {
		return false, 0
	}
	return true, time.Until(recent[0].Add(l.window))
}

func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[key] = append(l.prune(key), time.Now())
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

func (l *Limiter) prune(key string) []time.Time {
	cut := time.Now().Add(-l.window)
	var out []time.Time
	for _, t := range l.fails[key] {
		if t.After(cut) {
			out = append(out, t)
		}
	}
	l.fails[key] = out
	return out
}
