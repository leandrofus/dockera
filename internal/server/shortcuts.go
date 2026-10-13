package server

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"ducktainer/internal/config"
	"ducktainer/internal/i18n"
	"ducktainer/internal/shortcuts"
)

// Start menu entries for the web ports of this machine's containers (the
// endpoints whose daemon runs here). They open http(s)://localhost:<port>.

type shortcutCandidate struct {
	config.ShortcutRef
	Name    string `json:"name"`
	Stack   string `json:"stack,omitempty"`
	Service string `json:"service,omitempty"`
	URL     string `json:"url"`
	On      bool   `json:"on"`
	Running bool   `json:"running"`
}

func refKey(r config.ShortcutRef) string {
	return r.Endpoint + "/" + r.Container + "/" + strconv.Itoa(r.Port)
}

// thisMachine lists the endpoints served by a daemon on this machine.
func (s *Server) thisMachine() []string {
	var out []string
	for _, ep := range s.Cfg.Snapshot().Endpoints {
		if ep.Local() || strings.HasPrefix(ep.URL, "wsl://") {
			out = append(out, ep.Name)
		}
	}
	return out
}

func localURL(e jumpEntry) string {
	if e.URL != "" {
		return e.URL
	}
	host := "localhost"
	if ip, err := netip.ParseAddr(e.HostIP); err == nil && !ip.IsUnspecified() && !ip.IsLoopback() {
		host = ip.String()
	}
	scheme := e.Scheme
	if scheme != "https" {
		scheme = "http"
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(e.PublicPort))
}

// shortcutCandidates: every published web port of the running containers
// of this machine, plus the chosen ones that are not running right now.
func (s *Server) shortcutCandidates(ctx context.Context) []shortcutCandidate {
	sc := s.Cfg.Snapshot().Shortcuts
	if sc == nil {
		sc = &config.Shortcuts{}
	}
	var out []shortcutCandidate
	for _, ep := range s.thisMachine() {
		res, err := s.jumpEntries(ctx, ep, false)
		if err != nil {
			log.Printf("shortcuts: %s: %v", ep, err)
			continue
		}
		seen := map[string]bool{}
		for _, e := range res.Entries {
			if e.Kind != "http" || e.Proto != "tcp" || e.PublicPort == 0 {
				continue
			}
			ref := config.ShortcutRef{Endpoint: ep, Container: e.Container, Port: e.Port}
			if seen[refKey(ref)] {
				continue
			}
			seen[refKey(ref)] = true
			c := shortcutCandidate{ShortcutRef: ref, Stack: e.Stack, Service: e.Service, URL: localURL(e), Running: true}
			c.On = sc.All || slices.Contains(sc.Items, ref)
			out = append(out, c)
		}
	}
	for _, ref := range sc.Items {
		if !slices.ContainsFunc(out, func(c shortcutCandidate) bool { return c.ShortcutRef == ref }) {
			out = append(out, shortcutCandidate{ShortcutRef: ref, On: true})
		}
	}
	// names: "stack · service" (a bare "app" or "web" says little in a
	// menu), the container otherwise; the port tells repeated ones apart
	base := func(c shortcutCandidate) string {
		switch {
		case c.Service == "":
			return c.Container
		case c.Stack == "" || c.Stack == c.Service:
			return c.Service
		}
		return c.Stack + " · " + c.Service
	}
	count := map[string]int{}
	for _, c := range out {
		count[base(c)]++
	}
	for i := range out {
		c := &out[i]
		c.Name = base(*c)
		if count[c.Name] > 1 {
			c.Name += " :" + strconv.Itoa(c.Port)
		}
	}
	slices.SortFunc(out, func(a, b shortcutCandidate) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}

func (s *Server) syncShortcuts(ctx context.Context) error {
	if !shortcuts.Detect().Supported {
		return nil
	}
	sc := s.Cfg.Snapshot().Shortcuts
	if sc == nil || (!sc.All && len(sc.Items) == 0) {
		return shortcuts.Sync(nil, nil)
	}
	var items []shortcuts.Item
	var keep []string
	for _, c := range s.shortcutCandidates(ctx) {
		switch {
		case !c.On:
		case c.Running:
			items = append(items, shortcuts.Item{Key: refKey(c.ShortcutRef), Name: c.Name, URL: c.URL})
		default:
			keep = append(keep, refKey(c.ShortcutRef)) // stopped: leave its entry
		}
	}
	return shortcuts.Sync(items, keep)
}

// StartShortcuts keeps the menu in step with the containers (ports and
// names change when they are recreated).
func (s *Server) StartShortcuts() {
	if !shortcuts.Detect().Supported {
		return
	}
	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			if err := s.syncShortcuts(ctx); err != nil {
				log.Printf("shortcuts: %v", err)
			}
			cancel()
			time.Sleep(2 * time.Minute)
		}
	}()
}

func (s *Server) shortcutsInfo(w http.ResponseWriter, r *http.Request) {
	info := shortcuts.Detect()
	if info.Reason != "" {
		info.Reason = i18n.T(reqLang(r), info.Reason)
	}
	out := map[string]any{"info": info, "all": false, "candidates": []shortcutCandidate{}}
	if sc := s.Cfg.Snapshot().Shortcuts; sc != nil {
		out["all"] = sc.All
	}
	if info.Supported {
		if c := s.shortcutCandidates(r.Context()); c != nil {
			out["candidates"] = c
		}
	}
	writeJSON(w, out)
}

func (s *Server) putShortcuts(w http.ResponseWriter, r *http.Request) {
	var body config.Shortcuts
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if info := shortcuts.Detect(); !info.Supported {
		writeErr(w, badRequest(info.Reason))
		return
	}
	local := s.thisMachine()
	var items []config.ShortcutRef
	for _, it := range body.Items {
		if !slices.Contains(local, it.Endpoint) {
			writeErr(w, badRequest("%s is not on this machine: shortcuts only open containers of the machine running ducktainer", it.Endpoint))
			return
		}
		if it.Container != "" && it.Port > 0 && !slices.Contains(items, it) {
			items = append(items, it)
		}
	}
	err := s.Cfg.Update(func(c *config.Config) error {
		c.Shortcuts = &config.Shortcuts{All: body.All, Items: items}
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.syncShortcuts(r.Context()); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
