// Package shortcuts puts the web ports of local containers in the desktop's
// start menu: a "Containers" submenu on Linux (freedesktop .desktop entries)
// and Start › Programs › Containers on Windows (.url Internet shortcuts).
// Only for the machine running ducktainer, and only when it has a desktop.
package shortcuts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"ducktainer/internal/config"
)

// Item is one shortcut. Key identifies it across syncs.
type Item struct {
	Key  string
	Name string
	URL  string
}

// Info says whether shortcuts can be created here and where they go.
type Info struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"` // why not (English, translatable)
	Dir       string `json:"dir,omitempty"`
	Scope     string `json:"scope,omitempty"` // user | system
	Menu      string `json:"menu,omitempty"`  // where people find them
	OS        string `json:"os"`
	// Refreshed: the desktop's menu picks up changes by itself (on Linux
	// the system menu needs the watcher `ducktainer setup` installs).
	Refreshed bool `json:"refreshed"`
}

// Detect inspects the desktop (cheap: files and environment only).
func Detect() Info { return detect() }

var mu sync.Mutex

func manifestPath() string { return filepath.Join(config.Dir(), "shortcuts.json") }

// manifest maps keys to the files written for them, so only ducktainer's
// own files are ever replaced or deleted.
type manifest map[string]string

func loadManifest() manifest {
	m := manifest{}
	if data, err := os.ReadFile(manifestPath()); err == nil {
		json.Unmarshal(data, &m)
	}
	return m
}

func (m manifest) save() error {
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(manifestPath(), data, 0o600); err != nil {
		return err
	}
	config.FixOwner(manifestPath())
	return nil
}

// Sync makes the menu hold exactly items, plus the keys in keep that were
// written before (containers that are stopped for now keep their entry).
func Sync(items []Item, keep []string) error {
	mu.Lock()
	defer mu.Unlock()
	info := detect()
	if !info.Supported {
		return errors.New(info.Reason)
	}
	if err := os.MkdirAll(info.Dir, 0o755); err != nil {
		return err
	}
	if err := prepare(info); err != nil {
		return err
	}
	old := loadManifest()
	next := manifest{}
	used := map[string]bool{}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	var errs []error
	changed := false
	for _, it := range items {
		name := fileName(it, used)
		used[strings.ToLower(name)] = true
		path := filepath.Join(info.Dir, name)
		content, err := render(it)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if prev, err := os.ReadFile(path); err != nil || string(prev) != content {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				errs = append(errs, err)
				continue
			}
			changed = true
		}
		next[it.Key] = name
	}
	for _, k := range keep {
		if f, ok := old[k]; ok && next[k] == "" && !used[strings.ToLower(f)] {
			next[k] = f
			used[strings.ToLower(f)] = true
		}
	}
	for k, f := range old {
		if next[k] != f && !used[strings.ToLower(f)] && filepath.Base(f) == f {
			if os.Remove(filepath.Join(info.Dir, f)) == nil {
				changed = true
			}
		}
	}
	if changed {
		refreshMenu(info)
	}
	if err := next.save(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// safeName keeps file names portable (Windows forbids <>:"/\|?*).
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '-'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.Trim(s, ". ")
	if s == "" {
		s = "container"
	}
	return s
}
