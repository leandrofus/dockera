// Package setup adapts the systemd service to the local Docker install:
// which socket the daemon uses and how an unprivileged service can reach
// it. Nothing about the docker group/user is assumed.
package setup

import (
	"bufio"
	"dockera/internal/i18n"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const DropIn = "/etc/systemd/system/dockera.service.d/10-docker-access.conf"

type Mode string

const (
	ModeGroup Mode = "group" // socket readable by a group: join it
	ModeUser  Mode = "user"  // rootless daemon: run as the socket owner
	ModeRoot  Mode = "root"  // only root can reach the socket
	ModeNone  Mode = "none"  // no Docker found
)

type Plan struct {
	Socket  string      `json:"socket"`
	Exists  bool        `json:"exists"`
	Source  string      `json:"source"` // how the socket was found
	Owner   string      `json:"owner"`
	Group   string      `json:"group"`
	Perm    fs.FileMode `json:"perm"`
	Mode    Mode        `json:"mode"`
	RunUser string      `json:"run_user,omitempty"`
	Notes   []string    `json:"notes,omitempty"`
}

func (p *Plan) note(f string, a ...any) { p.Notes = append(p.Notes, i18n.Sprintf(f, a...)) }

// candidate sockets, most specific first
func candidates(explicit string) [][2]string {
	var out [][2]string
	if explicit != "" {
		out = append(out, [2]string{strings.TrimPrefix(explicit, "unix://"), "--socket"})
	}
	if h := envFileDockerHost("/etc/dockera/dockera.env"); h != "" {
		out = append(out, [2]string{h, "/etc/dockera/dockera.env"})
	}
	if h := strings.TrimPrefix(os.Getenv("DOCKER_HOST"), "unix://"); h != "" && h != os.Getenv("DOCKER_HOST") {
		out = append(out, [2]string{h, "DOCKER_HOST"})
	}
	out = append(out, [2]string{"/var/run/docker.sock", "standard path"}, [2]string{"/run/docker.sock", "standard path"})
	if m, _ := filepath.Glob("/run/user/*/docker.sock"); len(m) == 1 {
		out = append(out, [2]string{m[0], "rootless Docker"})
	}
	out = append(out, [2]string{"/run/podman/podman.sock", "Podman"})
	return out
}

func envFileDockerHost(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "DOCKER_HOST="); ok {
			v = strings.Trim(v, `"'`)
			if s, ok := strings.CutPrefix(v, "unix://"); ok {
				return s
			}
		}
	}
	return ""
}

// socketUnit reads ListenStream/SocketGroup from docker.socket, for when the
// daemon is installed but its socket doesn't exist yet.
func socketUnit() (path, group string) {
	for _, f := range []string{"/etc/systemd/system/docker.socket", "/usr/lib/systemd/system/docker.socket", "/lib/systemd/system/docker.socket"} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			if v, ok := strings.CutPrefix(l, "ListenStream="); ok && strings.HasPrefix(v, "/") {
				path = v
			}
			if v, ok := strings.CutPrefix(l, "SocketGroup="); ok {
				group = v
			}
		}
		if path != "" {
			return path, group
		}
	}
	return "", ""
}

func nameOfUser(uid int) string {
	if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
		return u.Username
	}
	return strconv.Itoa(uid)
}

func nameOfGroup(gid int) string {
	if g, err := user.LookupGroupId(strconv.Itoa(gid)); err == nil {
		return g.Name
	}
	return strconv.Itoa(gid)
}

// Detect inspects the system and decides how the service reaches Docker.
func Detect(explicit string) *Plan {
	p := &Plan{Mode: ModeNone}
	for _, c := range candidates(explicit) {
		st, err := os.Stat(c[0])
		if err != nil || st.Mode()&fs.ModeSocket == 0 {
			if c[1] == "--socket" || c[1] == "/etc/dockera/dockera.env" {
				p.note("%s (%s) does not exist or is not a socket", c[0], c[1])
			}
			continue
		}
		uid, gid, _ := owner(st)
		p.Socket, p.Exists, p.Source = c[0], true, c[1]
		p.Owner, p.Group, p.Perm = nameOfUser(uid), nameOfGroup(gid), st.Mode().Perm()
		switch {
		case uid != 0:
			p.Mode, p.RunUser = ModeUser, p.Owner
			p.note("the socket belongs to %s (rootless Docker): the service will run as that user", p.Owner)
		case gid != 0 && p.Perm&0o060 == 0o060:
			p.Mode = ModeGroup
			p.note("access through group %s (socket %s:%s %04o)", p.Group, p.Owner, p.Group, p.Perm)
		default:
			p.Mode = ModeRoot
			p.note("socket %s:%s %04o is only accessible by root: the service will run as root", p.Owner, p.Group, p.Perm)
			if c[1] == "Podman" {
				p.note("with Podman you can also enable a socket with a group, see podman.socket(5)")
			}
		}
		return p
	}
	// installed but not running yet
	if path, group := socketUnit(); path != "" {
		p.Socket, p.Source = path, "docker.socket (not created yet)"
		if group != "" && group != "root" {
			p.Mode, p.Group = ModeGroup, group
			p.note("Docker has not created its socket yet; its unit sets group %s", group)
		} else {
			p.Mode = ModeRoot
			p.note("Docker has not created its socket yet and its unit sets no group: the service will run as root")
		}
		return p
	}
	p.note("no Docker daemon found; install Docker and run `sudo dockera setup`")
	return p
}

// DropInContent renders the systemd drop-in for a plan ("" = none needed).
func (p *Plan) DropInContent() string {
	var b strings.Builder
	b.WriteString("# Generated by `dockera setup` from the detected Docker installation.\n")
	b.WriteString("# Do not edit: run `sudo dockera setup` again if Docker changes.\n")
	fmt.Fprintf(&b, "# modo=%s socket=%s (%s)\n[Service]\n", p.Mode, p.Socket, p.Source)
	if p.Socket != "" {
		fmt.Fprintf(&b, "Environment=DOCKER_HOST=unix://%s\n", p.Socket)
	}
	switch p.Mode {
	case ModeGroup:
		fmt.Fprintf(&b, "SupplementaryGroups=%s\n", p.Group)
	case ModeUser:
		// keep group dockera so the PAM helper socket stays reachable
		fmt.Fprintf(&b, "User=%s\nGroup=dockera\n", p.RunUser)
	case ModeRoot:
		b.WriteString("User=root\nGroup=dockera\n")
	case ModeNone:
		return ""
	}
	return b.String()
}

// Apply writes (or removes) the drop-in and reloads systemd.
func (p *Plan) Apply(restart bool) error {
	if os.Geteuid() != 0 {
		return i18n.Errorf("run as root: sudo dockera setup")
	}
	content := p.DropInContent()
	if content == "" {
		if err := os.Remove(DropIn); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(DropIn), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(DropIn, []byte(content), 0o644); err != nil {
			return err
		}
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return nil // not booted with systemd (containers, chroots)
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %v: %s", err, out)
	}
	if restart {
		exec.Command("systemctl", "try-restart", "dockera.service").Run()
	}
	return nil
}
