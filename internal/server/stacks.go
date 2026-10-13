package server

import (
	"context"
	"dockler/internal/i18n"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"dockler/internal/compose"
	"dockler/internal/docker"
)

const (
	lblProject = "com.docker.compose.project"
	lblService = "com.docker.compose.service"
	lblWorkDir = "com.docker.compose.project.working_dir"
	lblFiles   = "com.docker.compose.project.config_files"
	lblEnvFile = "com.docker.compose.project.environment_file"
)

type stackContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Service string `json:"service"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Image   string `json:"image"`
	Tty     bool   `json:"-"`
}

type stackInfo struct {
	Name        string           `json:"name"`
	Managed     bool             `json:"managed"`
	WorkingDir  string           `json:"working_dir,omitempty"`
	ConfigFiles []string         `json:"config_files,omitempty"`
	EnvFile     string           `json:"env_file,omitempty"`
	Services    []string         `json:"services"`
	Containers  []stackContainer `json:"containers"`
	Running     int              `json:"running"`
	Total       int              `json:"total"`
	Status      string           `json:"status"` // running | partial | stopped | empty
	Editable    bool             `json:"editable"`
	// Writable: files can be saved back (Editable only means readable).
	Writable bool `json:"writable"`
	// Reason explains why the compose files can't be edited from here.
	Reason string `json:"reason,omitempty"`
}

type rawContainer struct {
	ID     string `json:"Id"`
	Names  []string
	Image  string
	State  string
	Status string
	Labels map[string]string
}

func (s *Server) stacks(ctx context.Context, c *docker.Client, epName, lang string) (map[string]*stackInfo, error) {
	var cts []rawContainer
	if err := c.Get(ctx, "/containers/json", url.Values{"all": {"1"}, "filters": {`{"label":["` + lblProject + `"]}`}}, &cts); err != nil {
		return nil, err
	}
	local := strings.HasPrefix(c.Endpoint.URL, "unix://")
	out := map[string]*stackInfo{}
	for _, ct := range cts {
		p := ct.Labels[lblProject]
		st := out[p]
		if st == nil {
			st = &stackInfo{Name: p}
			out[p] = st
		}
		if st.WorkingDir == "" {
			st.WorkingDir = ct.Labels[lblWorkDir]
			if f := ct.Labels[lblFiles]; f != "" {
				st.ConfigFiles = strings.Split(f, ",")
			}
			st.EnvFile = ct.Labels[lblEnvFile]
		}
		st.Containers = append(st.Containers, stackContainer{ID: ct.ID, Name: name(ct.Names), Service: ct.Labels[lblService], State: ct.State, Status: ct.Status, Image: ct.Image})
	}
	for _, n := range s.Stacks.Managed(epName) {
		if out[n] == nil {
			out[n] = &stackInfo{Name: n}
		}
		out[n].Managed = true
	}
	for _, st := range out {
		svc := map[string]bool{}
		for _, ct := range st.Containers {
			svc[ct.Service] = true
			if ct.State == "running" {
				st.Running++
			}
		}
		for k := range svc {
			st.Services = append(st.Services, k)
		}
		sort.Strings(st.Services)
		sort.Slice(st.Containers, func(i, j int) bool { return st.Containers[i].Name < st.Containers[j].Name })
		st.Total = len(st.Containers)
		switch {
		case st.Total == 0:
			st.Status = "empty"
		case st.Running == st.Total:
			st.Status = "running"
		case st.Running == 0:
			st.Status = "stopped"
		default:
			st.Status = "partial"
		}
		st.Editable, st.Reason = editable(st, local, lang)
		st.Writable = st.Editable
		if st.Editable && !st.Managed {
			for _, f := range compose.ExistingFiles(st.ConfigFiles) {
				if !compose.Writable(f) {
					st.Writable, st.Reason = false, i18n.T(lang, "no write permission on %s for user %s (read-only)", f, runUser())
					break
				}
			}
		}
		if st.Containers == nil {
			st.Containers = []stackContainer{}
		}
		if st.Services == nil {
			st.Services = []string{}
		}
	}
	return out, nil
}

func editable(st *stackInfo, local bool, lang string) (bool, string) {
	switch {
	case st.Managed:
		return true, ""
	case !local:
		return false, i18n.T(lang, "the project was created directly on the remote host; its files are not reachable from dockler (you can adopt it by pasting the compose)")
	case len(st.ConfigFiles) == 0:
		return false, i18n.T(lang, "the containers do not record their compose files")
	}
	for _, f := range compose.ExistingFiles(st.ConfigFiles) {
		if _, err := os.Stat(f); err != nil {
			if os.IsPermission(err) {
				return false, i18n.T(lang, "user %s cannot access %s (check the directory permissions)", runUser(), f)
			}
			return false, i18n.T(lang, "%s not found (was the project moved or deleted?)", f)
		}
		if !compose.Readable(f) {
			return false, i18n.T(lang, "no read permission on %s for user %s", f, runUser())
		}
	}
	return true, ""
}

func runUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "?"
}

func (s *Server) listStacks(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	m, err := s.stacks(r.Context(), c, r.PathValue("ep"), reqLang(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	list := make([]*stackInfo, 0, len(m))
	for _, st := range m {
		list = append(list, st)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	writeJSON(w, list)
}

func (s *Server) stack(r *http.Request) (*docker.Client, *stackInfo, error) {
	c, err := s.ep(r)
	if err != nil {
		return nil, nil, err
	}
	m, err := s.stacks(r.Context(), c, r.PathValue("ep"), reqLang(r))
	if err != nil {
		return nil, nil, err
	}
	st, ok := m[r.PathValue("name")]
	if !ok {
		return nil, nil, herr(http.StatusNotFound, "stack not found: %s", r.PathValue("name"))
	}
	return c, st, nil
}

// files returns the compose files for a stack, if reachable from here.
func (s *Server) files(ep string, st *stackInfo) (*compose.Files, error) {
	if st.Managed {
		return s.Stacks.Read(ep, st.Name)
	}
	if !st.Editable {
		return nil, errors.New(st.Reason)
	}
	return compose.ReadExternal(st.WorkingDir, st.ConfigFiles, st.EnvFile)
}

func (s *Server) getStack(w http.ResponseWriter, r *http.Request) {
	_, st, err := s.stack(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	resp := map[string]any{"stack": st}
	if f, err := s.files(r.PathValue("ep"), st); err == nil {
		resp["files"] = f
	} else {
		resp["files_error"] = i18n.Render(err, reqLang(r))
	}
	writeJSON(w, resp)
}

func (s *Server) runner(ep string) (*compose.Runner, error) {
	e, ok := s.Cfg.Endpoint(ep)
	if !ok {
		return nil, herr(http.StatusNotFound, "unknown endpoint")
	}
	return &compose.Runner{Endpoint: e, Registries: s.Cfg.Snapshot().Registry}, nil
}

// compose runs docker compose for a stack streaming its output.
func (s *Server) compose(ctx context.Context, st *stream, ep, project string, f *compose.Files, args ...string) error {
	rn, err := s.runner(ep)
	if err != nil {
		return err
	}
	lw := &lineWriter{s: st}
	defer lw.Flush()
	dir, files := os.TempDir(), []string(nil)
	if f != nil {
		dir, files = f.Dir, f.Paths()
		// a custom --env-file used at creation must be passed again
		if !f.Managed && f.EnvPath != "" && f.EnvPath != filepath.Join(f.Dir, ".env") {
			args = append([]string{"--env-file", f.EnvPath}, args...)
		}
	}
	if err := rn.Run(ctx, lw, project, dir, files, args...); err != nil {
		lw.Flush()
		if lw.last != "" && !strings.HasPrefix(lw.last, "$ docker") {
			return fmt.Errorf("%s (%w)", lw.last, err)
		}
		return err
	}
	return nil
}

type stackReq struct {
	Name    string `json:"name"`
	Compose string `json:"compose"`
	// Files holds edited contents by path (multi-file external projects).
	Files  map[string]string `json:"files"`
	Env    string            `json:"env"`
	Deploy bool              `json:"deploy"`
	Pull   bool              `json:"pull"`
}

func (s *Server) createStack(w http.ResponseWriter, r *http.Request) {
	ep := r.PathValue("ep")
	var req stackReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	if err := compose.ValidName(req.Name); err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	if s.Stacks.Exists(ep, req.Name) {
		writeErr(w, herr(http.StatusConflict, "a stack with that name already exists"))
		return
	}
	if _, err := s.Stacks.Write(ep, req.Name, req.Compose, req.Env); err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	ctx := context.WithoutCancel(r.Context())
	st.Close(func() error {
		f, err := s.Stacks.Read(ep, req.Name)
		if err != nil {
			return err
		}
		if err := s.compose(ctx, st, ep, req.Name, f, "config", "--quiet"); err != nil {
			s.Stacks.Delete(ep, req.Name)
			return i18n.Errorf("invalid compose, not saved: %v", err)
		}
		st.Log("Stack saved")
		if !req.Deploy {
			return nil
		}
		args := []string{"up", "-d", "--remove-orphans"}
		if req.Pull {
			args = append(args, "--pull", "always")
		}
		return s.compose(ctx, st, ep, req.Name, f, args...)
	}())
}

func (s *Server) updateStack(w http.ResponseWriter, r *http.Request) {
	ep := r.PathValue("ep")
	name := r.PathValue("name")
	var req stackReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	var f *compose.Files
	var restore func()
	if s.Stacks.Exists(ep, name) {
		prev, _ := s.Stacks.Read(ep, name)
		content := req.Compose
		if content == "" {
			for _, v := range req.Files {
				content = v
			}
		}
		if _, err := s.Stacks.Write(ep, name, content, req.Env); err != nil {
			writeErr(w, err)
			return
		}
		f, _ = s.Stacks.Read(ep, name)
		restore = func() { s.Stacks.Write(ep, name, prev.Files[0].Content, prev.Env) }
	} else {
		_, info, err := s.stack(r)
		if err != nil {
			writeErr(w, err)
			return
		}
		prev, err := s.files(ep, info)
		if err != nil {
			writeErr(w, err)
			return
		}
		if !info.Writable {
			writeErr(w, &httpError{http.StatusForbidden, errors.New(info.Reason)})
			return
		}
		if err := compose.WriteExternal(prev, req.Files, req.Env); err != nil {
			writeErr(w, err)
			return
		}
		f, _ = compose.ReadExternal(info.WorkingDir, info.ConfigFiles, info.EnvFile)
		old := map[string]string{}
		for _, x := range prev.Files {
			old[x.Path] = x.Content
		}
		restore = func() { compose.WriteExternal(f, old, prev.Env) }
	}
	st := newStream(w)
	ctx := context.WithoutCancel(r.Context())
	st.Close(func() error {
		if err := s.compose(ctx, st, ep, name, f, "config", "--quiet"); err != nil {
			restore()
			return i18n.Errorf("invalid compose, the previous version was restored: %v", err)
		}
		st.Log("Changes saved")
		if !req.Deploy {
			return nil
		}
		args := []string{"up", "-d", "--remove-orphans"}
		if req.Pull {
			args = append(args, "--pull", "always")
		}
		return s.compose(ctx, st, ep, name, f, args...)
	}())
}

func (s *Server) stackAction(w http.ResponseWriter, r *http.Request) {
	ep := r.PathValue("ep")
	_, info, err := s.stack(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	f, ferr := s.files(ep, info)
	var args []string
	needsFiles := false
	switch r.PathValue("action") {
	case "up":
		args, needsFiles = []string{"up", "-d", "--remove-orphans"}, true
	case "redeploy":
		args, needsFiles = []string{"up", "-d", "--remove-orphans", "--pull", "always", "--force-recreate"}, true
	case "pull":
		args, needsFiles = []string{"pull"}, true
	case "down":
		args = []string{"down", "--remove-orphans"}
	case "stop", "start", "restart", "pause", "unpause":
		args = []string{r.PathValue("action")}
	default:
		writeErr(w, badRequest("unknown action"))
		return
	}
	if needsFiles && ferr != nil {
		writeErr(w, badRequest(ferr.Error()))
		return
	}
	if ferr != nil {
		f = nil
	}
	st := newStream(w)
	st.Close(s.compose(context.WithoutCancel(r.Context()), st, ep, info.Name, f, args...))
}

func (s *Server) deleteStack(w http.ResponseWriter, r *http.Request) {
	ep := r.PathValue("ep")
	name := r.PathValue("name")
	_, info, err := s.stack(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	st.Close(func() error {
		ctx := context.WithoutCancel(r.Context())
		if info.Total > 0 {
			args := []string{"down", "--remove-orphans"}
			if r.URL.Query().Get("volumes") == "1" {
				args = append(args, "--volumes")
			}
			f, ferr := s.files(ep, info)
			if ferr != nil {
				f = nil
			}
			if err := s.compose(ctx, st, ep, name, f, args...); err != nil {
				return err
			}
		}
		if info.Managed {
			if err := s.Stacks.Delete(ep, name); err != nil {
				return err
			}
			st.Log("Stack files removed")
		}
		return nil
	}())
}

// adoptStack copies an external project's files into dockler's store so it
// can be managed (and edited) from here.
func (s *Server) adoptStack(w http.ResponseWriter, r *http.Request) {
	ep := r.PathValue("ep")
	_, info, err := s.stack(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if info.Managed {
		writeErr(w, badRequest("the stack is already managed by dockler"))
		return
	}
	var body struct {
		Compose string `json:"compose"`
		Env     string `json:"env"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if body.Compose == "" {
		f, err := s.files(ep, info)
		if err != nil {
			writeErr(w, badRequest("%v — paste the compose content to adopt it", err))
			return
		}
		body.Env = f.Env
		body.Compose = f.Files[0].Content
		if len(f.Files) > 1 { // merge base + overrides into one file, keeping ${VARS}
			merged, err := s.mergedConfig(r.Context(), ep, info.Name, f)
			if err != nil {
				writeErr(w, err)
				return
			}
			body.Compose = merged
		}
	}
	if _, err := s.Stacks.Write(ep, info.Name, body.Compose, body.Env); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// stackLogs merges the log streams of every container in a project.
func (s *Server) stackLogs(w http.ResponseWriter, r *http.Request) {
	c, info, err := s.stack(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	q := logQuery(r)
	svc := r.URL.Query().Get("service")
	var wg sync.WaitGroup
	for _, ct := range info.Containers {
		if svc != "" && ct.Service != svc {
			continue
		}
		var in struct {
			Config struct{ Tty bool }
		}
		if err := c.Get(r.Context(), "/containers/"+ct.ID+"/json", nil, &in); err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			label := ct.Service
			if label == "" {
				label = ct.Name
			}
			if err := streamLogs(r.Context(), c, ct.ID, in.Config.Tty, q, label, st); err != nil && r.Context().Err() == nil {
				b, _ := json.Marshal(logLine{S: "err", M: "[dockler] " + err.Error(), C: label})
				st.Raw(b)
			}
		}()
	}
	wg.Wait()
	st.Close(nil)
}

// mergedConfig returns `docker compose config --no-interpolate` for a
// multi-file project: one equivalent compose file with variables intact.
func (s *Server) mergedConfig(ctx context.Context, ep, project string, f *compose.Files) (string, error) {
	rn, err := s.runner(ep)
	if err != nil {
		return "", err
	}
	cmd, cleanup, err := rn.Command(ctx, project, f.Dir, f.Paths(), "config", "--no-interpolate")
	if err != nil {
		return "", err
	}
	defer cleanup()
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("compose config: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

func (s *Server) stackMerged(w http.ResponseWriter, r *http.Request) {
	_, info, err := s.stack(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	f, err := s.files(r.PathValue("ep"), info)
	if err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	merged, err := s.mergedConfig(r.Context(), r.PathValue("ep"), info.Name, f)
	if err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]string{"compose": merged})
}

// stackMerge folds base + override files into the base file in place.
func (s *Server) stackMerge(w http.ResponseWriter, r *http.Request) {
	ep := r.PathValue("ep")
	_, info, err := s.stack(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if info.Managed {
		writeErr(w, badRequest("dockler stacks already use a single file"))
		return
	}
	if !info.Writable {
		writeErr(w, &httpError{http.StatusForbidden, errors.New(info.Reason)})
		return
	}
	f, err := s.files(ep, info)
	if err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	merged, err := s.mergedConfig(r.Context(), ep, info.Name, f)
	if err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	moved, err := compose.Merge(f, merged)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "file": f.Files[0].Path, "backups": append([]string{f.Files[0].Path + compose.BackupSuffix}, moved...)})
}
