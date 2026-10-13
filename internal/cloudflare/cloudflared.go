// Package cloudflare runs cloudflared (quick and named tunnels) and talks to
// the Cloudflare API to manage a named tunnel, its routes and DNS records.
package cloudflare

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"ducktainer/internal/config"
	"ducktainer/internal/i18n"
)

// Version is the cloudflared release downloaded when none is installed.
const Version = "2026.9.3"

// checksums of the release assets (from the release notes).
var checksums = map[string]string{
	"linux/amd64":   "77e26d8d900e0b8469f416239d14b5f296525fdf79fee6f511ef55609e3fbac2",
	"linux/arm64":   "aaeb2d7d0da3614634c7e03ab13487a1522c2e79165ed2929cfe23d5e95b326d",
	"linux/arm":     "967dc371a3fedbf09e881c13ee7ba317155ebc336cbd4afb756b46fc6785e5af",
	"windows/amd64": "f096265ec2fcbe9bb6e2d64268db167ced3fcbb83d894bdb9e2fcdb26f2ea7e2",
}

func assetName() string {
	if runtime.GOOS == "windows" {
		return "cloudflared-windows-" + runtime.GOARCH + ".exe"
	}
	return "cloudflared-" + runtime.GOOS + "-" + runtime.GOARCH
}

func managedPath() string {
	name := "cloudflared"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(config.Dir(), "bin", name)
}

var binMu sync.Mutex

// Binary finds cloudflared: $DUCKTAINER_CLOUDFLARED, the copy downloaded to
// the data directory, or one on PATH. Empty when there is none.
func Binary() string {
	if p := os.Getenv("DUCKTAINER_CLOUDFLARED"); p != "" {
		return p
	}
	if st, err := os.Stat(managedPath()); err == nil && !st.IsDir() {
		return managedPath()
	}
	for _, p := range []string{"cloudflared", "/usr/local/bin/cloudflared", "/usr/bin/cloudflared"} {
		if lp, err := exec.LookPath(p); err == nil {
			return lp
		}
	}
	return ""
}

// Ensure returns cloudflared, downloading the pinned release (checksum
// verified) into the data directory when it is not installed.
func Ensure(ctx context.Context) (string, error) {
	binMu.Lock()
	defer binMu.Unlock()
	if p := Binary(); p != "" {
		return p, nil
	}
	sum, ok := checksums[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", i18n.Errorf("install cloudflared: there is no download for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	url := "https://github.com/cloudflare/cloudflared/releases/download/" + Version + "/" + assetName()
	log.Printf("cloudflared: downloading %s", url)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return "", i18n.Errorf("could not download cloudflared: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", i18n.Errorf("could not download cloudflared: %s", resp.Status)
	}
	dst := managedPath()
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return "", err
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o750)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 200<<20))
	f.Close()
	if err == nil && hex.EncodeToString(h.Sum(nil)) != sum {
		err = i18n.Errorf("the cloudflared download does not match its checksum")
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// Proc is a supervised cloudflared: it restarts with backoff until Stop.
type Proc struct {
	Name string
	Args []string
	Env  []string // extra variables (secrets stay out of the process list)
	// OnURL receives the trycloudflare.com address of a quick tunnel.
	OnURL func(string)
	// OnState receives "starting", "connected" or an error message.
	OnState func(state string)

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}
}

var (
	quickURL  = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
	connected = regexp.MustCompile(`Registered tunnel connection|Connection [0-9a-f-]+ registered`)
	errLine   = regexp.MustCompile(`\b(ERR|error)\b`)
)

func (p *Proc) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancel, p.done = cancel, make(chan struct{})
	p.mu.Unlock()
	go p.loop(ctx)
}

func (p *Proc) state(s string) {
	if p.OnState != nil {
		p.OnState(s)
	}
}

func (p *Proc) loop(ctx context.Context) {
	defer close(p.done)
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		p.state("starting")
		start := time.Now()
		err := p.run(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("cloudflared %s: %v", p.Name, err)
			p.state(err.Error())
		}
		if time.Since(start) > time.Minute {
			backoff = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 2*time.Minute)
	}
}

func (p *Proc) run(ctx context.Context) error {
	bin, err := Ensure(ctx)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, p.Args...)
	cmd.Env = append(append(os.Environ(), "NO_AUTOUPDATE=true"), p.Env...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	detach(cmd)
	out, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	cmd.Stdout = cmd.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	var last string
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		line := sc.Text()
		if u := quickURL.FindString(line); u != "" && p.OnURL != nil && !strings.Contains(line, "api.trycloudflare.com") {
			p.OnURL(u)
		}
		if connected.MatchString(line) {
			p.state("connected")
		} else if errLine.MatchString(line) {
			last = line
		}
	}
	err = cmd.Wait()
	if last != "" {
		// "2026-01-01T00:00:00Z ERR message" -> "message"
		if i := strings.Index(last, " ERR "); i >= 0 {
			last = last[i+5:]
		}
		return errors.New(strings.TrimSpace(last))
	}
	if err != nil {
		return fmt.Errorf("cloudflared exited: %v", err)
	}
	return errors.New("cloudflared exited")
}

// Stop ends the process and waits for it.
func (p *Proc) Stop() {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}
