//go:build linux

package shortcuts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncUserMenu(t *testing.T) {
	if systemMode() {
		t.Skip("runs as a user without the system directory")
	}
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("DUCKTAINER_HOME", filepath.Join(tmp, "home"))
	t.Setenv("DISPLAY", ":0")
	os.MkdirAll(filepath.Join(tmp, "home"), 0o700)
	dir := filepath.Join(tmp, "data", "applications", "ducktainer-containers")

	items := []Item{
		{Key: "local/grafana/3000", Name: "grafana", URL: "http://localhost:3000"},
		{Key: "local/app/80", Name: "app", URL: "http://localhost:8080/?q=100%25"},
	}
	if err := Sync(items, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ducktainer-local-app-80.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Exec=xdg-open http://localhost:8080/?q=100%%25\n") || !strings.Contains(string(data), "Categories=X-Ducktainer-Containers;") {
		t.Fatalf("entry:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(tmp, "config", "menus", "applications-merged", "ducktainer-containers.menu")); err != nil {
		t.Fatal("submenu not installed:", err)
	}
	// a foreign file in the directory survives; a stopped container is kept
	os.WriteFile(filepath.Join(dir, "other.desktop"), []byte("x"), 0o644)
	if err := Sync(items[:1], []string{"local/app/80"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ducktainer-local-app-80.desktop")); err != nil {
		t.Fatal("kept entry removed")
	}
	if err := Sync(nil, nil); err != nil {
		t.Fatal(err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(left) != 1 || filepath.Base(left[0]) != "other.desktop" {
		t.Fatalf("left: %v", left)
	}
	if _, err := render(Item{URL: "http://x/$(rm -rf ~)"}); err == nil {
		t.Fatal("shell characters must be refused")
	}
}
