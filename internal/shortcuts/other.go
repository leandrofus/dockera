//go:build !linux && !windows

package shortcuts

import "runtime"

func detect() Info {
	return Info{OS: runtime.GOOS, Reason: "start menu shortcuts are only available on Linux and Windows"}
}

func prepare(Info) error                         { return nil }
func fileName(it Item, _ map[string]bool) string { return safeName(it.Name) }
func render(Item) (string, error)                { return "", nil }

func InstallSystem() (bool, error) { return false, nil }

var refreshMenu = func(Info) {}
