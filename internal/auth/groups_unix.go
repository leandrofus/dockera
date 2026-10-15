//go:build !windows

package auth

import (
	"bufio"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// DefaultAdminGroups are the groups whose members administer ducktainer on a
// fresh install.
func DefaultAdminGroups() []string { return []string{"docker", "ducktainer-admins"} }

// getent runs `getent db [key]` (NSS: works with LDAP/SSSD), falling back
// to the local files when getent is unavailable.
func getent(db string, key ...string) []string {
	out, err := exec.Command("getent", append([]string{db}, key...)...).Output()
	if err != nil {
		if _, lerr := exec.LookPath("getent"); lerr == nil {
			return nil // getent ran: the key just doesn't exist
		}
		return fileEntries("/etc/"+db, key...)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func fileEntries(path string, key ...string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if len(key) == 0 || strings.HasPrefix(line, key[0]+":") {
			out = append(out, line)
		}
	}
	return out
}

// Group returns a group's explicit members plus the users whose primary
// group it is. Users that only exist in a directory with enumeration
// disabled may not appear as primary members.
func Group(name string) GroupInfo {
	g := GroupInfo{Name: name, Members: []string{}}
	lines := getent("group", name)
	if len(lines) == 0 || lines[0] == "" {
		return g
	}
	f := strings.Split(lines[0], ":")
	if len(f) < 4 {
		return g
	}
	g.Exists, g.GID = true, f[2]
	seen := map[string]bool{}
	for _, m := range strings.Split(f[3], ",") {
		if m = strings.TrimSpace(m); m != "" && !seen[m] {
			seen[m] = true
			g.Members = append(g.Members, m)
		}
	}
	for _, l := range getent("passwd") {
		p := strings.Split(l, ":")
		if len(p) > 3 && p[3] == g.GID && !seen[p[0]] {
			seen[p[0]] = true
			g.Members = append(g.Members, p[0])
		}
	}
	sort.Strings(g.Members)
	return g
}
