// Package compose stores compose stacks created from ducktainer and runs the
// `docker compose` CLI plugin against any configured endpoint.
package compose

import (
	"bytes"
	"context"
	"ducktainer/internal/i18n"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ducktainer/internal/config"
	"ducktainer/internal/docker"
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

// Managed lists stack names stored for an endpoint (in the store or
// pointing to a directory).
func (s *Store) Managed(endpoint string) []string {
	entries, _ := os.ReadDir(filepath.Join(s.Root, "stacks", endpoint))
	var out []string
	for _, e := range entries {
		if e.IsDir() && s.Exists(endpoint, e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Location places a stack's compose file and .env in a directory of its
// own (on the node, for remote endpoints) instead of ducktainer's store, so
// relative volumes, env_file and build contexts resolve there.
type Location struct {
	Dir string `json:"dir"`
	// File is the compose file name inside Dir (default compose.yaml).
	File string `json:"file,omitempty"`
	// EnvFile overrides Dir/.env (passed as --env-file).
	EnvFile string `json:"env_file,omitempty"`
}

func (l *Location) ComposePath() string {
	switch {
	case l.File == "":
		return path.Join(l.Dir, FileName)
	case path.IsAbs(l.File):
		return l.File
	}
	return path.Join(l.Dir, l.File)
}

func (l *Location) EnvPath() string {
	if l.EnvFile != "" {
		return l.EnvFile
	}
	return path.Join(l.Dir, ".env")
}

// Validate cleans the paths; they must be absolute on the node.
func (l *Location) Validate() error {
	l.Dir = strings.TrimRight(strings.TrimSpace(l.Dir), "/")
	l.EnvFile = strings.TrimSpace(l.EnvFile)
	if !path.IsAbs(l.Dir) || l.Dir == "/" {
		return i18n.Errorf("the directory must be an absolute path, e.g. /opt/myapp")
	}
	l.Dir = path.Clean(l.Dir)
	if l.EnvFile != "" {
		if !path.IsAbs(l.EnvFile) {
			l.EnvFile = path.Join(l.Dir, l.EnvFile)
		}
		l.EnvFile = path.Clean(l.EnvFile)
		if l.EnvFile == path.Join(l.Dir, ".env") {
			l.EnvFile = ""
		}
	}
	return nil
}

const locationFile = "location.json"

// Location returns where a stack lives, or nil when it is in the store.
func (s *Store) Location(endpoint, name string) *Location {
	data, err := os.ReadFile(filepath.Join(s.dir(endpoint, name), locationFile))
	if err != nil {
		return nil
	}
	var l Location
	if json.Unmarshal(data, &l) != nil || l.Dir == "" {
		return nil
	}
	return &l
}

// SetLocation points a stack at a directory, dropping any stored copy.
func (s *Store) SetLocation(endpoint, name string, l Location) error {
	if err := ValidName(name); err != nil {
		return err
	}
	d := s.dir(endpoint, name)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(l, "", "  ")
	if err := os.WriteFile(filepath.Join(d, locationFile), data, 0o600); err != nil {
		return err
	}
	os.Remove(filepath.Join(d, FileName))
	os.Remove(filepath.Join(d, ".env"))
	return nil
}

// ComposeNames are the file names `docker compose` looks for, in order.
var ComposeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}

// FindCompose returns the compose file name present in dir, if any.
func FindCompose(ctx context.Context, fsys FS, dir string) (string, error) {
	for _, n := range ComposeNames {
		ok, err := fsys.Exists(ctx, path.Join(dir, n))
		if err != nil {
			return "", err
		}
		if ok {
			return n, nil
		}
	}
	return "", nil
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
	// Managed: kept in ducktainer's store (not in place).
	Managed bool `json:"managed"`
	// Node: the files live on the remote node; compose runs there.
	Node bool `json:"node"`
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

// Exists: the stack is in the store or points to a directory.
func (s *Store) Exists(endpoint, name string) bool {
	return s.Stored(endpoint, name) || s.Location(endpoint, name) != nil
}

// Stored: the stack's compose file is kept in the store.
func (s *Store) Stored(endpoint, name string) bool {
	_, err := os.Stat(filepath.Join(s.dir(endpoint, name), FileName))
	return err == nil
}

// ReadExternal loads every compose file of a project that lives in place
// (base + overrides), e.g. using the paths recorded in its container labels.
func ReadExternal(ctx context.Context, fsys FS, workDir string, configFiles []string, envFile string) (*Files, error) {
	if len(configFiles) == 0 {
		return nil, i18n.Errorf("the project does not record its compose files")
	}
	f := &Files{Dir: workDir}
	_, f.Node = fsys.(*NodeFS)
	paths, err := ExistingFiles(ctx, fsys, configFiles)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		data, err := fsys.ReadFile(ctx, p)
		if err != nil {
			return nil, i18n.Errorf("cannot read %s: %v", p, err)
		}
		f.Files = append(f.Files, File{Path: p, Name: path.Base(p), Content: string(data)})
	}
	f.EnvPath = envFile
	if f.EnvPath == "" {
		f.EnvPath = path.Join(workDir, ".env")
	}
	env, _ := fsys.ReadFile(ctx, f.EnvPath)
	f.Env = string(env)
	return f, nil
}

// WriteExternal saves edited files of a project in place. Only paths that
// belong to the project can be written; untouched files are left alone and
// existing permissions are kept.
func WriteExternal(ctx context.Context, fsys FS, f *Files, contents map[string]string, env string) error {
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
		if err := fsys.WriteFile(ctx, path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	if env != f.Env {
		return fsys.WriteFile(ctx, f.EnvPath, []byte(env), 0o600)
	}
	return nil
}

// Runner executes docker compose with the environment needed to reach an
// endpoint.
type Runner struct {
	Endpoint   config.Endpoint
	Registries []config.Registry
	// Client and Auth run compose on the node itself (files in place there).
	Client *docker.Client
	Auth   func(image string) string
}

// Exec runs compose for a project. With files on a remote node it runs
// there; otherwise on this machine against the endpoint.
func (r *Runner) Exec(ctx context.Context, stdout, stderr io.Writer, project string, f *Files, args ...string) error {
	dir, files := os.TempDir(), []string(nil)
	if f != nil {
		dir, files = f.Dir, f.Paths()
		// a custom --env-file must be passed on every run
		if f.EnvPath != "" && f.EnvPath != path.Join(f.Dir, ".env") {
			args = append([]string{"--env-file", f.EnvPath}, args...)
		}
	}
	if f == nil || !f.Node {
		cmd, cleanup, err := r.Command(ctx, project, dir, files, args...)
		if err != nil {
			return err
		}
		defer cleanup()
		cmd.Stdout, cmd.Stderr = stdout, stderr
		fmt.Fprintf(stderr, "$ docker %s\n", strings.Join(cmd.Args[1:], " "))
		return cmd.Run()
	}
	if r.Client == nil {
		return i18n.Errorf("cannot run compose on %s", r.Endpoint.Name)
	}
	full := []string{"docker", "compose", "-p", project}
	for _, x := range files {
		full = append(full, "-f", x)
	}
	full = append(full, "--project-directory", dir)
	full = append(full, args...)
	fmt.Fprintf(stderr, "$ [%s] %s\n", r.Endpoint.Name, strings.Join(full, " "))
	var dirs []string
	for _, x := range files {
		dirs = append(dirs, path.Dir(x))
	}
	if f.EnvPath != "" {
		dirs = append(dirs, path.Dir(f.EnvPath))
	}
	return r.runOnNode(ctx, stdout, stderr, dir, dirs, full)
}

// Output runs compose and returns its stdout.
func (r *Runner) Output(ctx context.Context, project string, f *Files, args ...string) (string, error) {
	var out, errb bytes.Buffer
	if err := r.Exec(ctx, &out, &errb, project, f, args...); err != nil {
		msg := strings.TrimSpace(errb.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return "", fmt.Errorf("%s (%w)", msg, err)
	}
	return out.String(), nil
}

// Command builds `docker compose -p project [-f files...] args...`.
func (r *Runner) Command(ctx context.Context, project, dir string, files []string, args ...string) (*exec.Cmd, func(), error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, nil, i18n.Errorf("the `docker` CLI with the compose plugin is required on the machine running ducktainer")
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

// Stack runs `docker stack args...` (Swarm) on this machine against the
// endpoint. vars (KEY=value) are added to the environment, which is what
// `docker stack deploy` interpolates the compose file with.
func (r *Runner) Stack(ctx context.Context, stdout, stderr io.Writer, dir string, vars []string, args ...string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return i18n.Errorf("the `docker` CLI is required on the machine running ducktainer")
	}
	cmd := exec.CommandContext(ctx, "docker", append([]string{"stack"}, args...)...)
	cmd.Dir = dir
	env, cleanup, err := r.env()
	if err != nil {
		return err
	}
	defer cleanup()
	cmd.Env = append(env, vars...) // later entries win over the server's own
	cmd.Stdout, cmd.Stderr = stdout, stderr
	fmt.Fprintf(stderr, "$ docker %s\n", strings.Join(cmd.Args[1:], " "))
	return cmd.Run()
}

// ParseEnv reads a .env file into KEY=value entries (comments, blank lines
// and an `export ` prefix are skipped; surrounding quotes are removed).
func ParseEnv(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out = append(out, k+"="+v)
	}
	return out
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

	// Temporary DOCKER_CONFIG: the user's config plus ducktainer registries and
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
	for k, v := range r.auths() {
		auths[k] = v
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
	tmp, err := os.MkdirTemp("", "ducktainer-cfg-")
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
	if u.Scheme == "agent" || u.Scheme == "wsl" {
		// the docker CLI cannot reach these: serve the daemon on a private
		// local socket (a named pipe on Windows) for the duration of the run
		if r.Client == nil {
			os.RemoveAll(tmp)
			return nil, nil, i18n.Errorf("cannot run compose on %s", ep.Name)
		}
		ln, h, err := listenPrivate(tmp)
		if err != nil {
			os.RemoveAll(tmp)
			return nil, nil, err
		}
		toClose = ln
		go proxyDaemon(ln, r.Client)
		host = h
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

// auths returns the ducktainer registries in docker config.json form.
func (r *Runner) auths() map[string]any {
	auths := map[string]any{}
	for _, reg := range r.Registries {
		if !reg.For(r.Endpoint.Name) {
			continue
		}
		server := reg.Server
		if server == "" || server == "docker.io" {
			server = "https://index.docker.io/v1/"
		}
		auths[server] = map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(reg.Username + ":" + reg.Password))}
	}
	return auths
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
const BackupSuffix = ".ducktainer-bak"

// ExistingFiles drops files that Merge folded into the base file (they are
// still listed in container labels until the containers are recreated).
func ExistingFiles(ctx context.Context, fsys FS, paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		ok, err := fsys.Exists(ctx, p)
		if err != nil {
			return nil, err
		}
		if !ok {
			if bak, _ := fsys.Exists(ctx, p+BackupSuffix); bak {
				continue
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// Merge writes merged into the project's first file and moves every other
// file aside (<file>.ducktainer-bak), keeping a backup of the base too.
func Merge(ctx context.Context, fsys FS, f *Files, merged string) ([]string, error) {
	if len(f.Files) < 2 {
		return nil, i18n.Errorf("the project already uses a single file")
	}
	base := f.Files[0]
	if err := fsys.WriteFile(ctx, base.Path+BackupSuffix, []byte(base.Content), 0o644); err != nil {
		return nil, err
	}
	var moved []string
	for _, x := range f.Files[1:] {
		if err := fsys.Rename(ctx, x.Path, x.Path+BackupSuffix); err != nil {
			return moved, err
		}
		moved = append(moved, x.Path+BackupSuffix)
	}
	return moved, fsys.WriteFile(ctx, base.Path, []byte(merged), 0o644)
}

// proxyDaemon forwards every connection accepted on ln to the daemon.
func proxyDaemon(ln net.Listener, c *docker.Client) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			rconn, err := c.Dial(context.Background())
			if err != nil {
				return
			}
			defer rconn.Close()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(rconn, conn); done <- struct{}{} }()
			go func() { _, _ = io.Copy(conn, rconn); done <- struct{}{} }()
			<-done
		}()
	}
}
