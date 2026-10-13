// Package templates implements the app marketplace. It reads Portainer's
// app template format (v2/v3) so every existing template list works, plus
// a curated built-in catalogue with inline compose files.
package templates

import (
	"context"
	"dockera/internal/i18n"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"dockera/internal/config"
)

//go:embed builtin.json
var builtinJSON []byte

const (
	TypeContainer = 1
	TypeSwarm     = 2
	TypeCompose   = 3
)

type Volume struct {
	Container string `json:"container"`
	Bind      string `json:"bind,omitempty"`
	ReadOnly  bool   `json:"readonly,omitempty"`
}

type Option struct {
	Text    string `json:"text"`
	Value   string `json:"value"`
	Default bool   `json:"default,omitempty"`
}

type EnvVar struct {
	Name        string   `json:"name"`
	Label       string   `json:"label,omitempty"`
	Default     string   `json:"default,omitempty"`
	Description string   `json:"description,omitempty"`
	Preset      bool     `json:"preset,omitempty"`
	Select      []Option `json:"select,omitempty"`
}

type Label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Repository struct {
	URL       string `json:"url"`
	Stackfile string `json:"stackfile"`
}

type Template struct {
	ID            string      `json:"id"`
	Source        string      `json:"source"`
	Type          int         `json:"type"`
	Name          string      `json:"name,omitempty"`
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	Note          string      `json:"note,omitempty"`
	Logo          string      `json:"logo,omitempty"`
	Categories    []string    `json:"categories,omitempty"`
	Platform      string      `json:"platform,omitempty"`
	Image         string      `json:"image,omitempty"`
	Ports         []string    `json:"ports,omitempty"`
	Volumes       []Volume    `json:"volumes,omitempty"`
	Env           []EnvVar    `json:"env,omitempty"`
	Command       string      `json:"command,omitempty"`
	RestartPolicy string      `json:"restart_policy,omitempty"`
	Labels        []Label     `json:"labels,omitempty"`
	Privileged    bool        `json:"privileged,omitempty"`
	Interactive   bool        `json:"interactive,omitempty"`
	Hostname      string      `json:"hostname,omitempty"`
	Network       string      `json:"network,omitempty"`
	Repository    *Repository `json:"repository,omitempty"`
	// Compose is a dockera extension: inline compose file for type 3.
	Compose string `json:"compose,omitempty"`
}

type file struct {
	Version   string            `json:"version"`
	Templates []json.RawMessage `json:"templates"`
}

func parse(source string, data []byte) ([]Template, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		// Portainer v1 lists are a bare array.
		if err2 := json.Unmarshal(data, &f.Templates); err2 != nil {
			return nil, err
		}
	}
	out := make([]Template, 0, len(f.Templates))
	for i, raw := range f.Templates {
		var t Template
		// ids are numeric in Portainer files, ignore and assign our own
		var tmp map[string]json.RawMessage
		if json.Unmarshal(raw, &tmp) != nil {
			continue
		}
		delete(tmp, "id")
		clean, _ := json.Marshal(tmp)
		if err := json.Unmarshal(clean, &t); err != nil {
			continue
		}
		if t.Type == 0 {
			t.Type = TypeContainer
		}
		t.ID = fmt.Sprintf("%s:%d", source, i)
		t.Source = source
		out = append(out, t)
	}
	return out, nil
}

type Catalog struct {
	cfg   *config.Config
	mu    sync.Mutex
	cache map[string]cached
	http  *http.Client
}

type cached struct {
	at   time.Time
	list []Template
	err  error
}

func NewCatalog(cfg *config.Config) *Catalog {
	return &Catalog{cfg: cfg, cache: map[string]cached{}, http: &http.Client{Timeout: 30 * time.Second}}
}

type SourceStatus struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Count int    `json:"count"`
	Error string `json:"error,omitempty"`
}

// List returns all templates from the built-in catalogue and every enabled
// remote source. Remote sources are cached for 30 minutes.
func (c *Catalog) List(ctx context.Context, refresh bool) ([]Template, []SourceStatus) {
	builtin, _ := parse("builtin", builtinJSON)
	all := append([]Template(nil), builtin...)
	status := []SourceStatus{{Name: "dockera (built-in)", URL: "builtin", Count: len(builtin)}}
	snap := c.cfg.Snapshot()
	type res struct {
		i    int
		list []Template
		err  error
	}
	var srcs []config.TemplateSource
	for _, s := range snap.Templates {
		if s.Enabled {
			srcs = append(srcs, s)
		}
	}
	results := make([]res, len(srcs))
	var wg sync.WaitGroup
	for i, s := range srcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			list, err := c.fetch(ctx, s.URL, refresh)
			results[i] = res{i, list, err}
		}()
	}
	wg.Wait()
	for i, r := range results {
		st := SourceStatus{Name: srcs[i].Name, URL: srcs[i].URL, Count: len(r.list)}
		if r.err != nil {
			st.Error = r.err.Error()
		}
		status = append(status, st)
		all = append(all, r.list...)
	}
	return all, status
}

func (c *Catalog) fetch(ctx context.Context, u string, refresh bool) ([]Template, error) {
	c.mu.Lock()
	if e, ok := c.cache[u]; ok && !refresh && time.Since(e.at) < 30*time.Minute {
		c.mu.Unlock()
		return e.list, e.err
	}
	c.mu.Unlock()
	data, err := c.get(ctx, u)
	var list []Template
	if err == nil {
		list, err = parse(u, data)
	}
	c.mu.Lock()
	c.cache[u] = cached{at: time.Now(), list: list, err: err}
	c.mu.Unlock()
	return list, err
}

func (c *Catalog) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

func (c *Catalog) Get(ctx context.Context, id string) (*Template, error) {
	all, _ := c.List(ctx, false)
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, i18n.Errorf("template %q not found", id)
}

// ComposeFile returns the compose content for a stack template, downloading
// it from the repository when it isn't inline.
func (c *Catalog) ComposeFile(ctx context.Context, t *Template) (string, error) {
	if t.Compose != "" {
		return t.Compose, nil
	}
	if t.Repository == nil || t.Repository.URL == "" {
		return "", i18n.Errorf("template %q has no compose file", t.Title)
	}
	raw, err := rawURL(t.Repository.URL, t.Repository.Stackfile)
	if err != nil {
		return "", err
	}
	data, err := c.get(ctx, raw)
	return string(data), err
}

func rawURL(repo, file string) (string, error) {
	u, err := url.Parse(strings.TrimSuffix(strings.TrimSuffix(repo, "/"), ".git"))
	if err != nil {
		return "", err
	}
	file = strings.TrimPrefix(file, "/")
	switch u.Host {
	case "github.com", "www.github.com":
		return "https://raw.githubusercontent.com" + u.Path + "/HEAD/" + file, nil
	case "gitlab.com":
		return "https://gitlab.com" + u.Path + "/-/raw/HEAD/" + file, nil
	case "codeberg.org":
		return "https://codeberg.org" + u.Path + "/raw/branch/main/" + file, nil
	}
	return "", i18n.Errorf("repository not supported for direct download: %s", repo)
}
