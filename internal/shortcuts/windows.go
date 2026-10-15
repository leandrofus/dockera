//go:build windows

package shortcuts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func detect() Info {
	info := Info{OS: "windows", Menu: `Start › Programs › Containers`}
	win := os.Getenv("WINDIR")
	if win == "" {
		win = `C:\Windows`
	}
	// Server Core and containers have no shell
	if _, err := os.Stat(filepath.Join(win, "explorer.exe")); err != nil {
		info.Reason = "this machine has no graphical desktop"
		return info
	}
	// a service (SYSTEM) writes for every user; a person, in their own menu
	appdata := os.Getenv("APPDATA")
	if appdata != "" && !strings.Contains(strings.ToLower(appdata), `\systemprofile\`) {
		info.Scope, info.Dir = "user", filepath.Join(appdata, `Microsoft\Windows\Start Menu\Programs\Containers`)
	} else {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		info.Scope, info.Dir = "system", filepath.Join(pd, `Microsoft\Windows\Start Menu\Programs\Containers`)
	}
	info.Supported, info.Refreshed = true, true // Explorer watches the Start menu
	return info
}

func prepare(Info) error { return nil }

func fileName(it Item, used map[string]bool) string {
	base := safeName(it.Name)
	name := base + ".url"
	for i := 2; used[strings.ToLower(name)]; i++ {
		name = fmt.Sprintf("%s (%d).url", base, i)
	}
	return name
}

func render(it Item) (string, error) {
	if strings.ContainsAny(it.URL, "\r\n") {
		return "", fmt.Errorf("invalid URL %q", it.URL)
	}
	// no IconFile: Windows shows the default browser's icon
	return "[InternetShortcut]\r\nURL=" + it.URL + "\r\n", nil
}

// InstallSystem: nothing to prepare on Windows.
func InstallSystem() (bool, error) { return false, nil }

var refreshMenu = func(Info) {}
