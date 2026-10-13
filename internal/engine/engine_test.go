package engine

import (
	"archive/zip"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"ducktainer/internal/docker"
)

func TestLatestStatic(t *testing.T) {
	listing := `<a href="docker-17.09.0-ce.zip">x</a> <a href="docker-27.5.1.zip">x</a>
<a href="docker-28.0.4.zip">x</a> <a href="docker-28.10.0.zip">x</a> <a href="docker-28.9.3.zip">x</a>`
	v, err := LatestStatic(listing)
	if err != nil || v != "28.10.0" {
		t.Fatalf("LatestStatic = %q, %v; want 28.10.0", v, err)
	}
	if _, err := LatestStatic("<html></html>"); err == nil {
		t.Fatal("an empty listing must fail")
	}
}

func TestPickRootfs(t *testing.T) {
	sums := `aaa *ubuntu-noble-wsl-arm64-wsl.rootfs.tar.gz
bbb *ubuntu-noble-wsl-amd64-wsl.rootfs.tar.gz
ccc *ubuntu-noble-wsl-amd64-wsl.manifest`
	n, s, err := PickRootfs(sums, "amd64")
	if err != nil || n != "ubuntu-noble-wsl-amd64-wsl.rootfs.tar.gz" || s != "bbb" {
		t.Fatalf("PickRootfs = %q %q %v", n, s, err)
	}
	if n, _, _ := PickRootfs("ddd  ubuntu-24.04-amd64.wsl\n", "amd64"); n != "ubuntu-24.04-amd64.wsl" {
		t.Fatalf("the .wsl format was not picked: %q", n)
	}
}

func TestWSLOutput(t *testing.T) {
	// wsl.exe writes UTF-16LE
	raw := []byte{}
	for _, r := range "Ubuntu\r\nducktainer\r\n" {
		raw = append(raw, byte(r), 0)
	}
	list := DecodeUTF16(raw)
	if !HasDistro(list, "ducktainer") || HasDistro(list, "debian") {
		t.Fatalf("HasDistro on %q", list)
	}
	if DecodeUTF16([]byte("plain")) != "plain" {
		t.Fatal("UTF-8 output must pass through")
	}
	v := "  NAME          STATE           VERSION\n* Ubuntu        Running         2\n  ducktainer    Stopped         1\n"
	if distroVersion(v, "ducktainer") != 1 || distroVersion(v, "Ubuntu") != 2 || distroVersion(v, "x") != 0 {
		t.Fatal("distroVersion")
	}
}

func TestExtractDocker(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "docker.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	for _, n := range []string{"docker/", "docker/dockerd.exe", "docker/docker.exe", "docker/LICENSE"} {
		w, _ := zw.Create(n)
		if n[len(n)-1] != '/' {
			w.Write([]byte(n))
		}
	}
	zw.Close()
	f.Close()
	got, err := ExtractDocker(zp, filepath.Join(dir, "out"))
	if err != nil || len(got) != 2 {
		t.Fatalf("ExtractDocker = %v, %v", got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "out", "dockerd.exe")); string(b) != "docker/dockerd.exe" {
		t.Fatalf("dockerd.exe content %q", b)
	}
}

func TestPipePath(t *testing.T) {
	u, _ := url.Parse(NativePipe)
	if p := docker.PipePath(u); p != `\\.\pipe\docker_engine` {
		t.Fatalf("PipePath = %q", p)
	}
}
