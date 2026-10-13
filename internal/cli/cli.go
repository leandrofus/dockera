// Package cli implements the terminal commands: ps, logs and sh.
package cli

import (
	"context"
	"ducktainer/internal/i18n"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"

	"golang.org/x/term"

	"ducktainer/internal/docker"
)

type container struct {
	ID     string `json:"Id"`
	Names  []string
	Image  string
	State  string
	Status string
	Labels map[string]string
	Ports  []struct {
		IP          string
		PrivatePort int
		PublicPort  int
		Type        string
	}
}

func (c container) name() string {
	if len(c.Names) == 0 {
		return c.ID[:12]
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

func list(ctx context.Context, c *docker.Client) ([]container, error) {
	var cts []container
	err := c.Get(ctx, "/containers/json", url.Values{"all": {"1"}}, &cts)
	return cts, err
}

const (
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
	dim    = "\x1b[2m"
	bold   = "\x1b[1m"
	reset  = "\x1b[0m"
)

func colorState(s string) string {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return s
	}
	switch s {
	case "running":
		return green + "● " + s + reset
	case "paused", "restarting":
		return yellow + "◐ " + s + reset
	default:
		return red + "○ " + s + reset
	}
}

// PS prints containers grouped by compose project.
func PS(ctx context.Context, c *docker.Client) error {
	cts, err := list(ctx, c)
	if err != nil {
		return err
	}
	groups := map[string][]container{}
	for _, ct := range cts {
		groups[ct.Labels["com.docker.compose.project"]] = append(groups[ct.Labels["com.docker.compose.project"]], ct)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "" || keys[j] == "" {
			return keys[j] == ""
		}
		return keys[i] < keys[j]
	})
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, k := range keys {
		title := i18n.Sprintf("stack %s", k)
		if k == "" {
			title = i18n.Sprintf("no stack")
		}
		fmt.Fprintf(tw, "\n%s%s%s\n", bold, title, reset)
		list := groups[k]
		sort.Slice(list, func(i, j int) bool { return list[i].name() < list[j].name() })
		for _, ct := range list {
			var ports []string
			for _, p := range ct.Ports {
				if p.PublicPort > 0 {
					ports = append(ports, fmt.Sprintf("%d→%d", p.PublicPort, p.PrivatePort))
				}
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s%s%s\t%s\n", ct.name(), colorState(ct.State), ct.Image, dim, ct.Status, reset, strings.Join(dedup(ports), " "))
		}
	}
	return tw.Flush()
}

func dedup(s []string) []string {
	seen := map[string]bool{}
	out := s[:0]
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// Resolve finds a container by exact name, id prefix, or unique substring.
func Resolve(ctx context.Context, c *docker.Client, q string) (container, error) {
	cts, err := list(ctx, c)
	if err != nil {
		return container{}, err
	}
	var matches []container
	for _, ct := range cts {
		if ct.name() == q || strings.HasPrefix(ct.ID, q) {
			return ct, nil
		}
		if strings.Contains(ct.name(), q) {
			matches = append(matches, ct)
		}
	}
	// prefer running containers when ambiguous
	if len(matches) > 1 {
		var running []container
		for _, m := range matches {
			if m.State == "running" {
				running = append(running, m)
			}
		}
		if len(running) == 1 {
			return running[0], nil
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return container{}, i18n.Errorf("no containers match %q", q)
	}
	var names []string
	for _, m := range matches {
		names = append(names, m.name())
	}
	return container{}, i18n.Errorf("%q is ambiguous: %s", q, strings.Join(names, ", "))
}

// Logs follows logs of a container, or of every container in a stack.
func Logs(ctx context.Context, c *docker.Client, target string, follow bool, tail string) error {
	cts, err := list(ctx, c)
	if err != nil {
		return err
	}
	var targets []container
	for _, ct := range cts {
		if ct.Labels["com.docker.compose.project"] == target {
			targets = append(targets, ct)
		}
	}
	if len(targets) == 0 {
		ct, err := Resolve(ctx, c, target)
		if err != nil {
			return err
		}
		targets = []container{ct}
	}
	colors := []string{"\x1b[36m", "\x1b[35m", "\x1b[34m", "\x1b[33m", "\x1b[32m", "\x1b[91m", "\x1b[96m"}
	width := 0
	for _, t := range targets {
		width = max(width, len(t.name()))
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, t := range targets {
		prefix := ""
		if len(targets) > 1 {
			prefix = fmt.Sprintf("%s%-*s |%s ", colors[i%len(colors)], width, t.name(), reset)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := streamLogs(ctx, c, t.ID, follow, tail, func(stream byte, line string) {
				mu.Lock()
				defer mu.Unlock()
				out := os.Stdout
				if stream == 2 {
					out = os.Stderr
				}
				fmt.Fprintf(out, "%s%s\n", prefix, line)
			}); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", t.name(), err)
			}
		}()
	}
	wg.Wait()
	return nil
}

func streamLogs(ctx context.Context, c *docker.Client, id string, follow bool, tail string, fn func(stream byte, line string)) error {
	var in struct{ Config struct{ Tty bool } }
	if err := c.Get(ctx, "/containers/"+id+"/json", nil, &in); err != nil {
		return err
	}
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "tail": {tail}, "follow": {strconv.FormatBool(follow)}}
	resp, err := c.Do(ctx, http.MethodGet, "/containers/"+id+"/logs", q, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	bufs := map[byte][]byte{}
	handle := func(stream byte, p []byte) error {
		b := append(bufs[stream], p...)
		for {
			i := strings.IndexByte(string(b), '\n')
			if i < 0 {
				break
			}
			fn(stream, strings.TrimRight(string(b[:i]), "\r"))
			b = b[i+1:]
		}
		bufs[stream] = b
		return nil
	}
	if in.Config.Tty {
		buf := make([]byte, 32<<10)
		for {
			n, err := resp.Body.Read(buf)
			handle(1, buf[:n])
			if err != nil {
				return nil
			}
		}
	}
	if err := docker.Demux(resp.Body, handle); err != nil && err != io.EOF {
		return err
	}
	return nil
}

// Shell opens an interactive exec session, picking bash/ash/sh
// automatically when no command is given.
func Shell(ctx context.Context, c *docker.Client, target, user string, cmd []string) error {
	ct, err := Resolve(ctx, c, target)
	if err != nil {
		return err
	}
	if ct.State != "running" {
		return i18n.Errorf("%s is not running (%s)", ct.name(), ct.State)
	}
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash; elif command -v ash >/dev/null 2>&1; then exec ash; else exec sh; fi"}
	}
	fd := int(os.Stdin.Fd())
	tty := term.IsTerminal(fd)
	cfg := map[string]any{
		"AttachStdin": true, "AttachStdout": true, "AttachStderr": true, "Tty": tty, "Cmd": cmd,
		"Env": []string{"TERM=" + envOr("TERM", "xterm-256color")},
	}
	if user != "" {
		cfg["User"] = user
	}
	var w, h int
	if tty {
		w, h, _ = term.GetSize(fd)
		cfg["ConsoleSize"] = []int{h, w}
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := c.JSON(ctx, http.MethodPost, "/containers/"+ct.ID+"/exec", nil, cfg, &created); err != nil {
		return err
	}
	conn, br, err := c.Hijack(ctx, http.MethodPost, "/exec/"+created.ID+"/start", map[string]any{"Tty": tty})
	if err != nil {
		return err
	}
	defer conn.Close()
	if tty {
		fmt.Fprintf(os.Stderr, "%s→ %s%s\n", dim, ct.name(), reset)
		old, err := term.MakeRaw(fd)
		if err != nil {
			return err
		}
		defer term.Restore(fd, old)
		resize := func() {
			if w, h, err := term.GetSize(fd); err == nil {
				c.JSON(ctx, http.MethodPost, "/exec/"+created.ID+"/resize", url.Values{"h": {strconv.Itoa(h)}, "w": {strconv.Itoa(w)}}, nil, nil)
			}
		}
		resize()
		stop := onResize(resize)
		defer stop()
	}
	go func() {
		io.Copy(conn, os.Stdin)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		}
	}()
	if tty {
		io.Copy(os.Stdout, br)
	} else {
		docker.Demux(br, func(stream byte, p []byte) error {
			if stream == 2 {
				os.Stderr.Write(p)
			} else {
				os.Stdout.Write(p)
			}
			return nil
		})
	}
	var info struct{ ExitCode int }
	c.Get(context.Background(), "/exec/"+created.ID+"/json", nil, &info)
	if info.ExitCode != 0 {
		return exitError(info.ExitCode)
	}
	return nil
}

type exitError int

func (e exitError) Error() string { return "exit " + strconv.Itoa(int(e)) }
func (e exitError) ExitCode() int { return int(e) }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// JSON prints v indented (used by `ducktainer inspect`).
func JSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
