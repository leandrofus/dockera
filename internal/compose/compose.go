// Package compose stores compose stacks created from dockera and runs the
// `docker compose` CLI plugin against any configured endpoint.
package compose

import (
	"context"
	"dockera/internal/i18n"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"dockera/internal/agent"
	"dockera/internal/config"
	"dockera/internal/docker"
)

const FileName = "compose.yaml"

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func ValidName(n string) error {
	if !validName.MatchString(n) {
		return i18n.Errorf("invalid stack name %q: use lowercase letters, digits, - and _", n)
	}
	return nil
}

type Store struct{ Root string }

func (s *Store) dir(endpoint, name string) string {
	return filepath.Join(s.Root, "stacks", endpoint, name)
}

// Managed lists stack names stored for an endpoint.
func (s *Store) Managed(endpoint string) []string {
	entries, _ := os.ReadDir(filepath.Join(s.Root, "stacks", endpoint))
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(s.dir(endpoint, e.Name()), FileName)); err == nil {
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// File is one compose file of a project, in -f order.
type File struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

type Files struct {
	Files   []File `json:"files"`
	Env     string `json:"env"`
	EnvPath string `json:"env_path"`
	Dir     string `json:"dir"`
	Managed bool   `json:"managed"`
}

// Paths returns the compose file paths in order.
func (f *Files) Paths() []string {
	out := make([]string, len(f.Files))
	for i, x := range f.Files {
		out[i] = x.Path
	}
	return out
}

func (s *Store) Read(endpoint, name string) (*Files, error) {
	d := s.dir(endpoint, name)
	path := filepath.Join(d, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	envPath := filepath.Join(d, ".env")
	env, _ := os.ReadFile(envPath)
	return &Files{Files: []File{{Path: path, Name: FileName, Content: string(data)}}, Env: string(env), EnvPath: envPath, Dir: d, Managed: true}, nil
}

func (s *Store) Write(endpoint, name, compose, env string) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	d := s.dir(endpoint, name)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(d, FileName), []byte(compose), 0o600); err != nil {
		return "", err
	}
	envPath := filepath.Join(d, ".env")
	if strings.TrimSpace(env) == "" {
		os.Remove(envPath)
	} else if err := os.WriteFile(envPath, []byte(env), 0o600); err != nil {
		return "", err
	}
	return d, nil
}

func (s *Store) Delete(endpoint, name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	return os.RemoveAll(s.dir(endpoint, name))
}

func (s *Store) Exists(endpoint, name string) bool {
	_, err := os.Stat(filepath.Join(s.dir(endpoint, name), FileName))
	return err == nil
}

// ReadExternal loads every compose file of a project started outside
// dockera (base + overrides), using the paths recorded in its container
// labels. Only works when those paths are visible from this machine.
func ReadExternal(workDir string, configFiles []string, envFile string) (*Files, error) {
	if len(configFiles) == 0 {
		return nil, i18n.Errorf("the project does not record its compose files")
	}
	f := &Files{Dir: workDir}
	for _, p := range ExistingFiles(configFiles) {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, i18n.Errorf("cannot read %s from this host: %v", p, err)
		}
		f.Files = append(f.Files, File{Path: p, Name: filepath.Base(p), Content: string(data)})
	}
	f.EnvPath = envFile
	if f.EnvPath == "" {
		f.EnvPath = filepath.Join(workDir, ".env")
	}
	env, _ := os.ReadFile(f.EnvPath)
	f.Env = string(env)
	return f, nil
}

// WriteExternal saves edited files of an external project in place. Only
// paths that belong to the project can be written; untouched files are left
// alone and existing permissions are kept.
func WriteExternal(f *Files, contents map[string]string, env string) error {
	for path, content := range contents {
		idx := -1
		for i, x := range f.Files {
			if x.Path == path {
				idx = i
			}
		}
		if idx < 0 {
			return i18n.Errorf("%s does not belong to the project", path)
		}
		if f.Files[idx].Content == content {
			continue
		}
		if err := writeKeepMode(path, content, 0o644); err != nil {
			return err
		}
	}
	if env != f.Env {
		return writeKeepMode(f.EnvPath, env, 0o600)
	}
	return nil
}

func writeKeepMode(path, content string, def os.FileMode) error {
	mode := def
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return os.WriteFile(path, []byte(content), mode)
}

// Runner executes docker compose with the environment needed to reach an
// endpoint.
type Runner struct {
	Endpoint   config.Endpoint
	Registries []config.Registry
}

// Command builds `docker compose -p project [-f files...] args...`.
func (r *Runner) Command(ctx context.Context, project, dir string, files []string, args ...string) (*exec.Cmd, func(), error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, nil, i18n.Errorf("the `docker` CLI with the compose plugin is required on the machine running dockera")
	}
	full := []string{"compose", "-p", project}
	for _, f := range files {
		full = append(full, "-f", f)
	}
	if dir != "" && len(files) > 0 {
		full = append(full, "--project-directory", dir)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Dir = dir
	env, cleanup, err := r.env()
	if err != nil {
		return nil, nil, err
	}
	cmd.Env = env
	return cmd, cleanup, nil
}

// Run executes and streams combined output to w.
func (r *Runner) Run(ctx context.Context, w io.Writer, project, dir string, files []string, args ...string) error {
	cmd, cleanup, err := r.Command(ctx, project, dir, files, args...)
	if err != nil {
		return err
	}
	defer cleanup()
	cmd.Stdout = w
	cmd.Stderr = w
	fmt.Fprintf(w, "$ docker %s\n", strings.Join(cmd.Args[1:], " "))
	return cmd.Run()
}

func (r *Runner) env() ([]string, func(), error) {
	env := os.Environ()
	filtered := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, "DOCKER_HOST=") && !strings.HasPrefix(e, "DOCKER_CONFIG=") &&
			!strings.HasPrefix(e, "DOCKER_TLS") && !strings.HasPrefix(e, "DOCKER_CONTEXT=") {
			filtered = append(filtered, e)
		}
	}
	env = filtered
	ep := r.Endpoint
	u, err := url.Parse(ep.URL)
	if err != nil {
		return nil, nil, err
	}
	host := ep.URL
	if u.Scheme == "http" || u.Scheme == "https" {
		host = "tcp://" + u.Host
	}

	// Temporary DOCKER_CONFIG: the user's config plus dockera registries and
	// the endpoint token header.
	cfg := map[string]any{}
	home, _ := os.UserHomeDir()
	if data, err := os.ReadFile(filepath.Join(home, ".docker", "config.json")); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}
	auths, _ := cfg["auths"].(map[string]any)
	if auths == nil {
		auths = map[string]any{}
	}
	for _, reg := range r.Registries {
		server := reg.Server
		if server == "" || server == "docker.io" {
			server = "https://index.docker.io/v1/"
		}
		auths[server] = map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(reg.Username + ":" + reg.Password))}
	}
	cfg["auths"] = auths
	if ep.Token != "" {
		hdrs, _ := cfg["HttpHeaders"].(map[string]any)
		if hdrs == nil {
			hdrs = map[string]any{}
		}
		hdrs["Authorization"] = "Bearer " + ep.Token
		cfg["HttpHeaders"] = hdrs
	}
	tmp, err := os.MkdirTemp("", "dockera-cfg-")
	if err != nil {
		return nil, nil, err
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(tmp, "config.json"), data, 0o600); err != nil {
		os.RemoveAll(tmp)
		return nil, nil, err
	}
	// Keep CLI plugins (compose) discoverable from the temporary config dir.
	if _, err := os.Stat(filepath.Join(home, ".docker", "cli-plugins")); err == nil {
		_ = os.Symlink(filepath.Join(home, ".docker", "cli-plugins"), filepath.Join(tmp, "cli-plugins"))
	}
	var toClose io.Closer
	if u.Scheme == "agent" {
		agentName := u.Host
		if agentName == "" {
			agentName = u.Path
		}
		agentName = strings.TrimPrefix(agentName, "/")
		if agentName == "" {
			agentName = ep.Name
		}
		sockPath := filepath.Join(tmp, "docker.sock")
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			os.RemoveAll(tmp)
			return nil, nil, err
		}
		toClose = ln
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					rconn, err := agent.DefaultHub.Dial(context.Background(), agentName)
					if err != nil {
						return
					}
					defer rconn.Close()
					done := make(chan struct{}, 2)
					go func() { _, _ = io.Copy(rconn, c); done <- struct{}{} }()
					go func() { _, _ = io.Copy(c, rconn); done <- struct{}{} }()
					<-done
				}(conn)
			}
		}()
		host = "unix://" + sockPath
	}
	env = append(env, "DOCKER_HOST="+host)

	env = append(env, "DOCKER_CONFIG="+tmp)
	if ep.TLS || u.Scheme == "https" {
		tlsEnv, err := TLSEnv(context.Background(), ep, tmp)
		if err != nil {
			if toClose != nil {
				_ = toClose.Close()
			}
			os.RemoveAll(tmp)
			return nil, nil, err
		}
		env = append(env, tlsEnv...)
	}
	return env, func() {
		if toClose != nil {
			_ = toClose.Close()
		}
		os.RemoveAll(tmp)
	}, nil
}

// TLSEnv writes the endpoint's (pinned) certificate as ca.pem in dir and
// returns the variables that make the docker CLI trust it. Verification is
// strict when the certificate covers the host name used.
func TLSEnv(ctx context.Context, ep config.Endpoint, dir string) ([]string, error) {
	certPEM, hostOK, err := docker.PeerCertPEM(ctx, ep)
	if err != nil {
		return nil, i18n.Errorf("TLS with %s: %v", ep.Name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.pem"), certPEM, 0o644); err != nil {
		return nil, err
	}
	env := []string{"DOCKER_CERT_PATH=" + dir}
	if hostOK {
		return append(env, "DOCKER_TLS_VERIFY=1"), nil
	}
	return append(env, "DOCKER_TLS=1"), nil
}

// BackupSuffix marks originals kept by Merge.
const BackupSuffix = ".dockera-bak"

// ExistingFiles drops files that Merge folded into the base file (they are
// still listed in container labels until the containers are recreated).
func ExistingFiles(paths []string) []string {
	var out []string
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			if _, berr := os.Stat(p + BackupSuffix); berr == nil {
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

// Merge writes merged into the project's first file and moves every other
// file aside (<file>.dockera-bak), keeping a backup of the base too.
func Merge(f *Files, merged string) ([]string, error) {
	if len(f.Files) < 2 {
		return nil, i18n.Errorf("the project already uses a single file")
	}
	base := f.Files[0]
	if err := os.WriteFile(base.Path+BackupSuffix, []byte(base.Content), 0o644); err != nil {
		return nil, err
	}
	var moved []string
	for _, x := range f.Files[1:] {
		if err := os.Rename(x.Path, x.Path+BackupSuffix); err != nil {
			return moved, err
		}
		moved = append(moved, x.Path+BackupSuffix)
	}
	return moved, writeKeepMode(base.Path, merged, 0o644)
}
