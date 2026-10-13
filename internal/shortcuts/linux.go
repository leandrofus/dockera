//go:build linux

package shortcuts

import (
	_ "embed"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

//go:embed ducktainer.svg
var icon []byte

// SystemDir is created (owned by the service user) by the packages; the
// unit lists it in ReadWritePaths. Menus scan subdirectories.
const SystemDir = "/usr/local/share/applications/ducktainer-containers"

const category = "X-Ducktainer-Containers"

func hasDesktop() bool {
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return true
	}
	// a service has no display: look for installed desktop sessions
	for _, d := range []string{"/usr/share/xsessions", "/usr/share/wayland-sessions"} {
		if m, _ := filepath.Glob(filepath.Join(d, "*.desktop")); len(m) > 0 {
			return true
		}
	}
	return false
}

// system: the service (it can write the directory `ducktainer setup`
// prepared, or runs as root / the ducktainer user) writes for every user of
// the machine; anyone else writes in their own menu.
func systemMode() bool {
	if syscall.Access(SystemDir, 2 /* W_OK */) == nil || os.Geteuid() == 0 {
		return true
	}
	u, err := user.Current()
	return err == nil && u.Username == "ducktainer"
}

// System-wide menu pieces installed by `ducktainer setup`.
var systemFiles = map[string]string{
	"/etc/xdg/menus/applications-merged/ducktainer-containers.menu":        MenuFile,
	"/usr/local/share/desktop-directories/ducktainer-containers.directory": DirectoryFile,
	"/usr/local/share/icons/hicolor/scalable/apps/ducktainer.svg":          string(icon),
}

// InstallSystem prepares the machine's menu for the service (root): the
// submenu, its icon and SystemDir owned by the service user. It does
// nothing on machines without a desktop.
func InstallSystem() (bool, error) {
	if !hasDesktop() {
		return false, nil
	}
	for p, data := range systemFiles {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			return false, err
		}
	}
	if err := os.MkdirAll(SystemDir, 0o755); err != nil {
		return false, err
	}
	if u, err := user.Lookup("ducktainer"); err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		if err := os.Chown(SystemDir, uid, gid); err != nil {
			return false, err
		}
	}
	return true, nil
}

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share")
}

func detect() Info {
	info := Info{OS: "linux", Menu: "Containers"}
	if !hasDesktop() {
		info.Reason = "this machine has no graphical desktop"
		return info
	}
	if systemMode() {
		info.Scope, info.Dir = "system", SystemDir
		if st, err := os.Stat(SystemDir); err != nil || !st.IsDir() {
			info.Reason = "the start menu is not prepared yet: run `sudo ducktainer setup`"
			return info
		}
	} else {
		info.Scope, info.Dir = "user", filepath.Join(dataHome(), "applications", "ducktainer-containers")
	}
	info.Supported = true
	return info
}

// prepare installs the submenu and the icon for a user (the packages ship
// them system-wide).
func prepare(info Info) error {
	if info.Scope != "user" {
		return nil
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		h, _ := os.UserHomeDir()
		cfg = filepath.Join(h, ".config")
	}
	files := map[string][]byte{
		filepath.Join(cfg, "menus", "applications-merged", "ducktainer-containers.menu"):    []byte(MenuFile),
		filepath.Join(dataHome(), "desktop-directories", "ducktainer-containers.directory"): []byte(DirectoryFile),
		filepath.Join(dataHome(), "icons", "hicolor", "scalable", "apps", "ducktainer.svg"): icon,
	}
	for p, data := range files {
		if prev, err := os.ReadFile(p); err == nil && string(prev) == string(data) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// MenuFile puts the entries in a "Containers" submenu (KDE, Xfce, MATE,
// Cinnamon, LXQt…; GNOME lists them in the app grid).
const MenuFile = `<!DOCTYPE Menu PUBLIC "-//freedesktop//DTD Menu 1.0//EN" "http://www.freedesktop.org/standards/menu-spec/1.0/menu.dtd">
<Menu>
  <Name>Applications</Name>
  <Menu>
    <Name>Containers</Name>
    <Directory>ducktainer-containers.directory</Directory>
    <Include>
      <Category>` + category + `</Category>
    </Include>
  </Menu>
</Menu>
`

const DirectoryFile = `[Desktop Entry]
Type=Directory
Name=Containers
Comment=Web apps running in containers (ducktainer)
Icon=ducktainer
`

func fileName(it Item, used map[string]bool) string {
	base := "ducktainer-" + strings.ToLower(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, it.Key))
	name := base + ".desktop"
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s-%d.desktop", base, i)
	}
	return name
}

// value escapes a desktop entry string value.
func value(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", " ", "\r", " ", "\t", " ").Replace(s)
}

func render(it Item) (string, error) {
	if strings.ContainsAny(it.URL, " \t\n\"'`$\\") {
		return "", fmt.Errorf("unsupported characters in %q", it.URL)
	}
	exec := "xdg-open " + strings.ReplaceAll(it.URL, "%", "%%")
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Version=1.0\n" +
		"Name=" + value(it.Name) + "\n" +
		"GenericName=Container\n" +
		"Comment=" + value(it.URL) + "\n" +
		"Exec=" + exec + "\n" +
		"Icon=ducktainer\n" +
		"Terminal=false\n" +
		"StartupNotify=false\n" +
		"Categories=" + category + ";Network;\n" +
		"Keywords=container;docker;ducktainer;\n" +
		"X-Ducktainer-Key=" + value(it.Key) + "\n", nil
}
