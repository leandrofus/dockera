// Package config holds dockera's persistent settings: access token, allowed
// hosts, docker endpoints, registries and marketplace sources.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"
)

type Endpoint struct {
	Name string `json:"name"`
	// URL is unix:///var/run/docker.sock, ssh://user@host[:port] or
	// tcp://host:port (a remote `dockera expose`).
	URL string `json:"url"`
	// Token for tcp endpoints exposed by `dockera expose`.
	Token string `json:"token,omitempty"`
	TLS   bool   `json:"tls,omitempty"`
	// Fingerprint pins the remote certificate (sha256, hex). Empty with TLS
	// on means "accept any" (still encrypted, not authenticated).
	Fingerprint string `json:"fingerprint,omitempty"`
	// SSHKey is an optional identity file for ssh endpoints.
	SSHKey string `json:"ssh_key,omitempty"`
}

type Registry struct {
	Name     string `json:"name"`
	Server   string `json:"server"` // e.g. docker.io, ghcr.io, registry.example.com:5000
	Username string `json:"username"`
	Password string `json:"password"`
}

type TemplateSource struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// Auth controls who can log in. System users (PAM) map to roles through
// their POSIX groups; the token is a break-glass admin credential.
type Auth struct {
	PAM          bool     `json:"pam"`
	PAMService   string   `json:"pam_service"`
	TokenLogin   bool     `json:"token_login"`
	AdminGroups  []string `json:"admin_groups"`
	ViewerGroups []string `json:"viewer_groups"`
	AdminUsers   []string `json:"admin_users"`
	ViewerUsers  []string `json:"viewer_users"`
	AllowRoot    bool     `json:"allow_root"`
	SessionHours int      `json:"session_hours"`
}

// DefaultAuth: members of `docker` can already control the daemon, so they
// are admins; extra groups are created by the packages.
func DefaultAuth(pamAvailable bool) *Auth {
	return &Auth{
		PAM:          pamAvailable,
		PAMService:   "dockera",
		TokenLogin:   true,
		AdminGroups:  []string{"docker", "dockera-admins"},
		ViewerGroups: []string{"dockera-viewers"},
		SessionHours: 12,
	}
}

// PAMAvailable is set by main from the auth package (avoids an import cycle).
var PAMAvailable bool

type Config struct {
	Token string `json:"token"`
	// SessionSecret signs login cookies; rotating it logs everyone out.
	SessionSecret string           `json:"session_secret"`
	Auth          *Auth            `json:"auth"`
	Allow         []string         `json:"allow"`
	Endpoints     []Endpoint       `json:"endpoints"`
	Registry      []Registry       `json:"registries"`
	Templates     []TemplateSource `json:"template_sources"`

	mu    sync.Mutex
	path  string
	mtime time.Time
}

var DefaultTemplateSources = []TemplateSource{
	{Name: "Portainer official", URL: "https://raw.githubusercontent.com/portainer/templates/v3/templates.json", Enabled: true},
	{Name: "Lissy93 (community, ~800 apps)", URL: "https://raw.githubusercontent.com/Lissy93/portainer-templates/main/templates.json", Enabled: false},
}

// Dir returns the data directory: $DOCKERA_HOME, /var/lib/dockera for root,
// or $XDG_CONFIG_HOME/dockera.
func Dir() string {
	if d := os.Getenv("DOCKERA_HOME"); d != "" {
		return d
	}
	if os.Geteuid() == 0 {
		return "/var/lib/dockera"
	}
	if u, err := user.Current(); err == nil && u.Username == "dockera" {
		return "/var/lib/dockera" // the packaged service user
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "dockera")
	}
	return ".dockera"
}

func NewToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func Load() (*Config, error) {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	c := &Config{path: filepath.Join(dir, "config.json")}
	data, err := os.ReadFile(c.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		c.Token = NewToken()
		c.SessionSecret = NewToken()
		c.Auth = DefaultAuth(PAMAvailable)
		c.Templates = append([]TemplateSource(nil), DefaultTemplateSources...)
		c.Endpoints = []Endpoint{{Name: "local", URL: defaultLocalURL()}}
		return c, c.Save()
	case err != nil:
		return nil, err
	}
	if err := c.decode(data); err != nil {
		return nil, err
	}
	return c, nil
}

// decode parses the file into c and fills defaults (callers hold c.mu or
// own c exclusively).
func (c *Config) decode(data []byte) error {
	var o Config
	if err := json.Unmarshal(data, &o); err != nil {
		return fmt.Errorf("%s: %w", c.path, err)
	}
	c.Token, c.SessionSecret, c.Auth = o.Token, o.SessionSecret, o.Auth
	c.Allow, c.Endpoints, c.Registry, c.Templates = o.Allow, o.Endpoints, o.Registry, o.Templates
	if c.Token == "" {
		c.Token = NewToken()
	}
	if c.SessionSecret == "" {
		c.SessionSecret = NewToken()
	}
	if c.Auth == nil { // configs from before system users existed
		c.Auth = DefaultAuth(PAMAvailable)
	}
	if c.Auth.PAMService == "" {
		c.Auth.PAMService = "dockera"
	}
	if c.Auth.SessionHours <= 0 {
		c.Auth.SessionHours = 12
	}
	if len(c.Endpoints) == 0 {
		c.Endpoints = []Endpoint{{Name: "local", URL: defaultLocalURL()}}
	}
	return nil
}

func (c *Config) Path() string { return c.path }

func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.save()
}

func (c *Config) save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	FixOwner(tmp)
	if err := os.Rename(tmp, c.path); err != nil {
		return err
	}
	if st, err := os.Stat(c.path); err == nil {
		c.mtime = st.ModTime()
	}
	return nil
}

// Update re-reads the file (another process — the CLI, the service — may
// have changed it), applies fn and persists the result.
func (c *Config) Update(fn func(c *Config) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if data, err := os.ReadFile(c.path); err == nil {
		if err := c.decode(data); err != nil {
			return err
		}
	}
	if err := fn(c); err != nil {
		return err
	}
	return c.save()
}

// Watch reloads the config when the file is changed by someone else and
// calls onChange afterwards (e.g. `sudo dockera allow add` while serving).
func (c *Config) Watch(onChange func()) {
	go func() {
		for range time.Tick(3 * time.Second) {
			st, err := os.Stat(c.path)
			if err != nil {
				continue
			}
			c.mu.Lock()
			changed := !st.ModTime().Equal(c.mtime)
			if changed {
				if data, err := os.ReadFile(c.path); err == nil && c.decode(data) == nil {
					c.mtime = st.ModTime()
				} else {
					changed = false
				}
			}
			c.mu.Unlock()
			if changed {
				onChange()
			}
		}
	}()
}

// FixOwner gives files written by root (sudo dockera …) to the owner of
// the data directory, so a service running as an unprivileged user can
// still read them.
func FixOwner(path string) {
	if os.Geteuid() != 0 {
		return
	}
	if uid, gid, ok := owner(filepath.Dir(path)); ok && uid != 0 {
		_ = os.Lchown(path, uid, gid)
	}
}

// Snapshot returns a copy safe to read without holding the lock.
func (c *Config) Snapshot() *Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := *c.Auth
	return &Config{
		Token:         c.Token,
		SessionSecret: c.SessionSecret,
		Auth:          &a,
		Allow:         append([]string(nil), c.Allow...),
		Endpoints:     append([]Endpoint(nil), c.Endpoints...),
		Registry:      append([]Registry(nil), c.Registry...),
		Templates:     append([]TemplateSource(nil), c.Templates...),
	}
}

func (c *Config) Endpoint(name string) (Endpoint, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.Endpoints {
		if e.Name == name {
			return e, true
		}
	}
	return Endpoint{}, false
}

func defaultLocalURL() string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return h
	}
	return "unix:///var/run/docker.sock"
}
