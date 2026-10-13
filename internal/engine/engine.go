// Package engine installs a Docker Engine on Windows without Docker
// Desktop, in one of two ways:
//
//   - native: the static dockerd.exe/docker.exe from download.docker.com,
//     registered as the "docker" Windows service (Windows containers),
//     reached through the npipe:////./pipe/docker_engine named pipe;
//   - wsl: a WSL 2 distribution of its own (Ubuntu) running docker.io with
//     systemd (Linux containers), reached through wsl:// endpoints.
//
// Downloads are verified: the Ubuntu image against Ubuntu's SHA256SUMS and
// the compose plugin against the checksum published with each release.
// download.docker.com publishes no checksums for the Windows static
// binaries; they are fetched over HTTPS and --sha256 can pin the archive.
package engine

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"ducktainer/internal/i18n"
)

// Options for Install.
type Options struct {
	Mode string // "native" or "wsl"
	// Distro is the WSL distribution created for Docker (wsl mode).
	Distro string
	// Version of the static Docker binaries (native mode, default latest).
	Version string
	// SHA256 pins the Docker zip (native mode).
	SHA256 string
	// NoKeepAlive skips the logon task that keeps the WSL distro running.
	NoKeepAlive bool
	Out         io.Writer
}

// Result tells the caller which endpoint reaches the new engine.
type Result struct {
	Name string // endpoint name
	URL  string // endpoint URL
	// Reboot: Windows must restart before the engine can run.
	Reboot bool
}

const (
	// NativePipe is the endpoint of the native engine.
	NativePipe = "npipe:////./pipe/docker_engine"
	// DefaultDistro is the WSL distribution created for Docker.
	DefaultDistro = "ducktainer"

	staticIndex = "https://download.docker.com/win/static/stable/"
	ubuntuWSL   = "https://cloud-images.ubuntu.com/wsl/releases/24.04/current/"
	composeAPI  = "https://api.github.com/repos/docker/compose/releases/latest"
)

var httpClient = &http.Client{Timeout: 30 * time.Minute}

// staticArch is the directory of download.docker.com for this machine.
func staticArch() string {
	if runtime.GOARCH == "arm64" {
		return "aarch64"
	}
	return "x86_64"
}

var dockerZip = regexp.MustCompile(`docker-(\d+)\.(\d+)\.(\d+)\.zip`)

// LatestStatic picks the highest docker-X.Y.Z.zip in a directory listing.
func LatestStatic(listing string) (string, error) {
	type v struct {
		s     string
		parts [3]int
	}
	var all []v
	for _, m := range dockerZip.FindAllStringSubmatch(listing, -1) {
		var x v
		x.s = fmt.Sprintf("%s.%s.%s", m[1], m[2], m[3])
		for i := 0; i < 3; i++ {
			x.parts[i], _ = strconv.Atoi(m[i+1])
		}
		all = append(all, x)
	}
	if len(all) == 0 {
		return "", i18n.Errorf("no Docker release found in %s", staticIndex)
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i].parts, all[j].parts
		for k := 0; k < 3; k++ {
			if a[k] != b[k] {
				return a[k] > b[k]
			}
		}
		return false
	})
	return all[0].s, nil
}

// PickRootfs finds the Ubuntu WSL image for arch in a SHA256SUMS file and
// returns its file name and checksum.
func PickRootfs(sums, arch string) (name, sum string, err error) {
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 {
			continue
		}
		n := strings.TrimPrefix(f[1], "*")
		if strings.Contains(n, arch) && (strings.HasSuffix(n, ".rootfs.tar.gz") || strings.HasSuffix(n, ".wsl")) {
			return n, f[0], nil
		}
	}
	return "", "", i18n.Errorf("no Ubuntu WSL image for %s in %s", arch, ubuntuWSL)
}

// ubuntuArch is the architecture name used by Ubuntu images.
func ubuntuArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

// composeAsset is the compose plugin release asset for this machine.
func composeAsset() string {
	if runtime.GOARCH == "arm64" {
		return "docker-compose-windows-aarch64.exe"
	}
	return "docker-compose-windows-x86_64.exe"
}

// fetch downloads url into memory (small files: listings, checksums).
func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(url, "https://api.github.com/") {
		req.Header.Set("Accept", "application/vnd.github+json")
		if t := os.Getenv("GITHUB_TOKEN"); t != "" {
			req.Header.Set("Authorization", "Bearer "+t)
		}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// download saves url as dst and checks its sha256 when want is set.
func download(ctx context.Context, out io.Writer, url, dst, want string) error {
	i18n.Fprintf(out, "Downloading %s…\n", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dst + ".part")
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst + ".part")
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); want != "" && !strings.EqualFold(got, want) {
		os.Remove(dst + ".part")
		return i18n.Errorf("checksum mismatch for %s: got %s, want %s", url, got, want)
	}
	os.Remove(dst)
	return os.Rename(dst+".part", dst)
}

// latestCompose returns the download URL and sha256 of the compose plugin.
func latestCompose(ctx context.Context) (url, sum string, err error) {
	data, err := fetch(ctx, composeAPI)
	if err != nil {
		return "", "", err
	}
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &rel); err != nil {
		return "", "", err
	}
	var sumURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case composeAsset():
			url = a.URL
		case composeAsset() + ".sha256":
			sumURL = a.URL
		}
	}
	if url == "" || sumURL == "" {
		return "", "", i18n.Errorf("the compose release has no %s", composeAsset())
	}
	s, err := fetch(ctx, sumURL)
	if err != nil {
		return "", "", err
	}
	f := strings.Fields(string(s))
	if len(f) == 0 {
		return "", "", errors.New("empty compose checksum")
	}
	return url, f[0], nil
}

// ExtractDocker copies the executables of the static Docker zip (stored
// under docker/) into dir.
func ExtractDocker(zipPath, dir string) ([]string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var out []string
	for _, f := range zr.File {
		name := filepath.Base(f.Name)
		if f.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(name), ".exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		dst := filepath.Join(dir, name)
		w, err := os.Create(dst)
		if err == nil {
			_, err = io.Copy(w, rc)
			if cerr := w.Close(); err == nil {
				err = cerr
			}
		}
		rc.Close()
		if err != nil {
			return nil, i18n.Errorf("cannot write %s (is Docker still running?): %v", dst, err)
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil, i18n.Errorf("%s has no executables", zipPath)
	}
	return out, nil
}

// DecodeUTF16 converts wsl.exe output, which is UTF-16LE, to a string;
// other output is returned as is.
func DecodeUTF16(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	}
	if len(b) < 2 || bytes.IndexByte(b, 0) < 0 {
		return string(b)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u))
}

// HasDistro reports whether `wsl -l -q` output lists name.
func HasDistro(list, name string) bool {
	for _, l := range strings.Split(list, "\n") {
		if strings.EqualFold(strings.TrimSpace(l), name) {
			return true
		}
	}
	return false
}

// distroVersion returns the WSL version (1 or 2) of a distribution from
// `wsl -l -v`, or 0 when it is not listed.
func distroVersion(list, name string) int {
	for _, l := range strings.Split(list, "\n") {
		f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(l), "*"))
		if len(f) >= 3 && strings.EqualFold(f[0], name) {
			switch f[len(f)-1] {
			case "1":
				return 1
			case "2":
				return 2
			}
		}
	}
	return 0
}

// wslConf turns on systemd (which starts dockerd) and makes root the
// default user of the distribution.
const wslConf = `[boot]
systemd=true

[user]
default=root
`

// provision installs Docker inside the distribution.
const provision = `set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq docker.io docker-compose-v2 >/dev/null
systemctl enable --now docker.service docker.socket
docker version --format 'docker {{.Server.Version}} is running in WSL'
`
