package server

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"ducktainer/internal/docker"
)

// splitImage returns registry host, repository (without tag) and tag.
func splitImage(ref string) (host, repo, tag string) {
	repo = ref
	if i := strings.Index(repo, "@"); i >= 0 {
		repo, tag = repo[:i], repo[i:]
	} else if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo, tag = repo[:i], repo[i+1:]
	}
	host = "docker.io"
	if first, _, ok := strings.Cut(repo, "/"); ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		host = first
	}
	return host, repo, tag
}

// registryAuth builds the X-Registry-Auth header for an image reference.
func (s *Server) registryAuth(ref string) string {
	host, _, _ := splitImage(ref)
	auth := map[string]string{}
	for _, reg := range s.Cfg.Snapshot().Registry {
		server := strings.TrimPrefix(strings.TrimPrefix(reg.Server, "https://"), "http://")
		server = strings.TrimSuffix(server, "/")
		if server == "" || server == "index.docker.io" || server == "registry-1.docker.io" {
			server = "docker.io"
		}
		if server == host {
			auth = map[string]string{"username": reg.Username, "password": reg.Password, "serveraddress": reg.Server}
			break
		}
	}
	data, _ := json.Marshal(auth)
	return base64.URLEncoding.EncodeToString(data)
}

// passthrough copies a docker JSON-lines progress stream to the client and
// returns the first error found inside it.
func passthrough(st *stream, body io.Reader) error {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var streamErr error
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var m struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(line, &m) == nil && m.Error != "" {
			streamErr = &httpError{http.StatusBadGateway, errors.New(m.Error)}
			continue
		}
		if err := st.Raw(line); err != nil {
			return err
		}
	}
	if streamErr != nil {
		return streamErr
	}
	return sc.Err()
}

func (s *Server) pull(ctx context.Context, c *docker.Client, st *stream, image string) error {
	_, repo, tag := splitImage(image)
	if tag == "" {
		tag = "latest"
	}
	q := url.Values{"fromImage": {repo}, "tag": {strings.TrimPrefix(tag, "@")}}
	if strings.HasPrefix(tag, "@") {
		q = url.Values{"fromImage": {repo + tag}}
	}
	resp, err := c.Do(ctx, http.MethodPost, "/images/create", q, nil, http.Header{"X-Registry-Auth": {s.registryAuth(image)}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return passthrough(st, resp.Body)
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	var images []map[string]any
	if err := c.Get(ctx, "/images/json", url.Values{"all": {"0"}}, &images); err != nil {
		writeErr(w, err)
		return
	}
	var containers []struct {
		ImageID string `json:"ImageID"`
		Names   []string
	}
	c.Get(ctx, "/containers/json", url.Values{"all": {"1"}}, &containers)
	usedBy := map[string][]string{}
	for _, ct := range containers {
		if len(ct.Names) > 0 {
			usedBy[ct.ImageID] = append(usedBy[ct.ImageID], strings.TrimPrefix(ct.Names[0], "/"))
		}
	}
	for _, im := range images {
		id, _ := im["Id"].(string)
		im["UsedBy"] = usedBy[id]
	}
	writeJSON(w, images)
}

func (s *Server) inspectImage(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	id := r.PathValue("id")
	var info, history json.RawMessage
	if err := c.Get(r.Context(), "/images/"+id+"/json", nil, &info); err != nil {
		writeErr(w, err)
		return
	}
	c.Get(r.Context(), "/images/"+id+"/history", nil, &history)
	writeJSON(w, map[string]any{"inspect": info, "history": history})
}

func (s *Server) pullImage(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Image string `json:"image"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Image) == "" {
		writeErr(w, badRequest("enter the image to pull"))
		return
	}
	st := newStream(w)
	st.Close(s.pull(r.Context(), c, st, strings.TrimSpace(body.Image)))
}

func (s *Server) pushImage(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Image string `json:"image"`
	}
	if err := readJSON(r, &body); err != nil || body.Image == "" {
		writeErr(w, badRequest("enter the image to push"))
		return
	}
	_, repo, tag := splitImage(body.Image)
	if tag == "" {
		tag = "latest"
	}
	st := newStream(w)
	resp, err := c.Do(r.Context(), http.MethodPost, "/images/"+repo+"/push", url.Values{"tag": {tag}}, nil, http.Header{"X-Registry-Auth": {s.registryAuth(body.Image)}})
	if err != nil {
		st.Close(err)
		return
	}
	defer resp.Body.Close()
	st.Close(passthrough(st, resp.Body))
}

func (s *Server) tagImage(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Source string `json:"source"`
		Target string `json:"target"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	_, repo, tag := splitImage(body.Target)
	if tag == "" {
		tag = "latest"
	}
	if err := c.JSON(r.Context(), http.MethodPost, "/images/"+body.Source+"/tag", url.Values{"repo": {repo}, "tag": {tag}}, nil, nil); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) removeImage(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var out json.RawMessage
	if err := c.JSON(r.Context(), http.MethodDelete, "/images/"+r.PathValue("id"), url.Values{"force": {r.URL.Query().Get("force")}}, nil, &out); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

func (s *Server) pruneImages(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	if r.URL.Query().Get("all") == "1" {
		q.Set("filters", `{"dangling":["false"]}`)
	}
	s.prune(w, r, "/images/prune", q)
}

func (s *Server) prune(w http.ResponseWriter, r *http.Request, p string, q url.Values) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var out json.RawMessage
	if err := c.JSON(r.Context(), http.MethodPost, p, q, nil, &out); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// buildImage builds from an inline Dockerfile (and optional extra files).
func (s *Server) buildImage(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Tag        string            `json:"tag"`
		Dockerfile string            `json:"dockerfile"`
		Files      map[string]string `json:"files"`
		Pull       bool              `json:"pull"`
		NoCache    bool              `json:"nocache"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(body.Dockerfile) == "" {
		writeErr(w, badRequest("the Dockerfile is empty"))
		return
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files := map[string]string{"Dockerfile": body.Dockerfile}
	for k, v := range body.Files {
		files[strings.TrimPrefix(k, "/")] = v
	}
	for name, content := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))})
		tw.Write([]byte(content))
	}
	tw.Close()
	q := url.Values{"t": {body.Tag}, "rm": {"1"}, "forcerm": {"1"}}
	if body.Pull {
		q.Set("pull", "1")
	}
	if body.NoCache {
		q.Set("nocache", "1")
	}
	st := newStream(w)
	resp, err := c.Do(r.Context(), http.MethodPost, "/build", q, io.Reader(&buf), nil)
	if err != nil {
		st.Close(err)
		return
	}
	defer resp.Body.Close()
	st.Close(passthrough(st, resp.Body))
}
