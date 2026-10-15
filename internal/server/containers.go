package server

import (
	"archive/tar"
	"bufio"
	"context"
	"ducktainer/internal/i18n"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"ducktainer/internal/docker"
)

func (s *Server) ep(r *http.Request) (*docker.Client, error) {
	return s.Client(r.PathValue("ep"))
}

// proxyJSON forwards a GET to the daemon and writes the JSON as is.
func (s *Server) proxyJSON(w http.ResponseWriter, r *http.Request, p string, q url.Values) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var out json.RawMessage
	if err := c.Get(r.Context(), p, q, &out); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

func (s *Server) listContainers(w http.ResponseWriter, r *http.Request) {
	q := url.Values{"all": {"1"}}
	if r.URL.Query().Get("size") == "1" {
		q.Set("size", "1")
	}
	s.proxyJSON(w, r, "/containers/json", q)
}

func (s *Server) inspectContainer(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	if r.URL.Query().Get("size") == "1" {
		q.Set("size", "1")
	}
	s.proxyJSON(w, r, "/containers/"+url.PathEscape(r.PathValue("id"))+"/json", q)
}

func (s *Server) containerTop(w http.ResponseWriter, r *http.Request) {
	s.proxyJSON(w, r, "/containers/"+url.PathEscape(r.PathValue("id"))+"/top", nil)
}

func (s *Server) inspectRaw(ctx context.Context, c *docker.Client, id string) (*inspect, error) {
	var in inspect
	err := c.Get(ctx, "/containers/"+url.PathEscape(id)+"/json", nil, &in)
	return &in, err
}

func (s *Server) containerSpec(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	in, err := s.inspectRaw(r.Context(), c, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, specFromInspect(in))
}

func (s *Server) containerAction(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	action := r.PathValue("action")
	switch action {
	case "start", "stop", "restart", "kill", "pause", "unpause":
	default:
		writeErr(w, badRequest("unknown action: %s", action))
		return
	}
	q := url.Values{}
	if t := r.URL.Query().Get("t"); t != "" {
		q.Set("t", t)
	}
	if sig := r.URL.Query().Get("signal"); sig != "" && action == "kill" {
		q.Set("signal", sig)
	}
	err = c.JSON(r.Context(), http.MethodPost, "/containers/"+url.PathEscape(r.PathValue("id"))+"/"+action, q, nil, nil)
	var ae *docker.APIError
	if errors.As(err, &ae) && ae.Status == http.StatusNotModified {
		err = nil
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) removeContainer(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	q := url.Values{"force": {r.URL.Query().Get("force")}, "v": {r.URL.Query().Get("v")}}
	if err := c.JSON(r.Context(), http.MethodDelete, "/containers/"+url.PathEscape(r.PathValue("id")), q, nil, nil); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) renameContainer(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := c.JSON(r.Context(), http.MethodPost, "/containers/"+url.PathEscape(r.PathValue("id"))+"/rename", url.Values{"name": {body.Name}}, nil, nil); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// updateContainer changes resources/restart policy live, no recreate.
func (s *Server) updateContainer(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body map[string]any
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	var out json.RawMessage
	if err := c.JSON(r.Context(), http.MethodPost, "/containers/"+url.PathEscape(r.PathValue("id"))+"/update", nil, body, &out); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) commitContainer(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Repo    string `json:"repo"`
		Tag     string `json:"tag"`
		Comment string `json:"comment"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	q := url.Values{"container": {r.PathValue("id")}, "repo": {body.Repo}, "tag": {body.Tag}, "comment": {body.Comment}}
	var out json.RawMessage
	if err := c.JSON(r.Context(), http.MethodPost, "/commit", q, map[string]any{}, &out); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// ---- create / recreate ----

type createReq struct {
	Spec  Spec `json:"spec"`
	Pull  bool `json:"pull"`
	Start bool `json:"start"`
}

func (s *Server) createContainer(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req createReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	body, err := createBody(req.Spec, nil, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	ctx := r.Context()
	st.Close(func() error {
		if err := s.ensureImage(ctx, c, st, req.Spec.Image, req.Pull); err != nil {
			return err
		}
		id, err := s.create(ctx, c, req.Spec.Name, body)
		if err != nil {
			return err
		}
		st.LogID(id, "Container created %s", short(id))
		if req.Start {
			if err := c.JSON(ctx, http.MethodPost, "/containers/"+id+"/start", nil, nil, nil); err != nil {
				return err
			}
			st.Log("Container started")
		}
		return nil
	}())
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func (s *Server) create(ctx context.Context, c *docker.Client, name string, body map[string]any) (string, error) {
	q := url.Values{}
	if name != "" {
		q.Set("name", name)
	}
	var out struct {
		ID       string   `json:"Id"`
		Warnings []string `json:"Warnings"`
	}
	if err := c.JSON(ctx, http.MethodPost, "/containers/create", q, body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// ensureImage pulls when asked or when the image is missing locally.
func (s *Server) ensureImage(ctx context.Context, c *docker.Client, st *stream, image string, force bool) error {
	if !force {
		err := c.Get(ctx, "/images/"+image+"/json", nil, nil)
		if err == nil {
			return nil
		}
		if !docker.IsNotFound(err) {
			return err
		}
	}
	st.Log("Pulling %s…", image)
	return s.pull(ctx, c, st, image)
}

// recreateContainer applies a new Spec: create the replacement, swap names,
// start it and remove the old one; rolls back if the new one fails to start.
func (s *Server) recreateContainer(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req createReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	old, err := s.inspectRaw(ctx, c, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	body, err := createBody(req.Spec, old.Config, old.HostConfig)
	if err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	// Run detached from the request so a closed tab can't leave a half-done swap.
	ctx = context.WithoutCancel(ctx)
	st.Close(s.swap(ctx, c, st, old, req, body))
}

func (s *Server) swap(ctx context.Context, c *docker.Client, st *stream, old *inspect, req createReq, body map[string]any) error {
	logf := st.Log
	oldName := strings.TrimPrefix(old.Name, "/")
	newName := req.Spec.Name
	if newName == "" {
		newName = oldName
	}
	if err := s.ensureImage(ctx, c, st, req.Spec.Image, req.Pull); err != nil {
		return err
	}
	tmpName := newName + "-ducktainer-new"
	newID, err := s.create(ctx, c, tmpName, body)
	if err != nil {
		return i18n.Errorf("could not create the replacement (the original was not modified): %v", err)
	}
	logf("Replacement created %s", short(newID))
	post := func(p string, q url.Values) error { return c.JSON(ctx, http.MethodPost, p, q, nil, nil) }
	if old.State.Running {
		logf("Stopping %s…", oldName)
		if err := post("/containers/"+old.ID+"/stop", url.Values{"t": {"15"}}); err != nil {
			c.JSON(ctx, http.MethodDelete, "/containers/"+newID, url.Values{"force": {"1"}}, nil, nil)
			return err
		}
	}
	backup := fmt.Sprintf("%s-ducktainer-old-%d", oldName, time.Now().Unix())
	if err := post("/containers/"+old.ID+"/rename", url.Values{"name": {backup}}); err != nil {
		return err
	}
	if err := post("/containers/"+newID+"/rename", url.Values{"name": {newName}}); err != nil {
		post("/containers/"+old.ID+"/rename", url.Values{"name": {oldName}})
		return err
	}
	if old.State.Running || req.Start {
		logf("Starting %s…", newName)
		if err := post("/containers/"+newID+"/start", nil); err != nil {
			logf("Start failed, rolling back: %v", err)
			c.JSON(ctx, http.MethodDelete, "/containers/"+newID, url.Values{"force": {"1"}}, nil, nil)
			post("/containers/"+old.ID+"/rename", url.Values{"name": {oldName}})
			if old.State.Running {
				post("/containers/"+old.ID+"/start", nil)
			}
			return i18n.Errorf("the new container did not start; the original was restored: %v", err)
		}
	}
	if err := c.JSON(ctx, http.MethodDelete, "/containers/"+old.ID, url.Values{"force": {"1"}}, nil, nil); err != nil {
		logf("Warning: could not remove the previous container %s: %v", backup, err)
	} else {
		logf("Previous container removed")
	}
	st.LogID(newID, "Done")
	return nil
}

func (s *Server) duplicateContainer(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req createReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	old, err := s.inspectRaw(r.Context(), c, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	// A duplicate must not inherit compose ownership.
	for k := range req.Spec.Labels {
		if strings.HasPrefix(k, "com.docker.compose.") {
			delete(req.Spec.Labels, k)
		}
	}
	body, err := createBody(req.Spec, old.Config, old.HostConfig)
	if err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	ctx := r.Context()
	st.Close(func() error {
		if err := s.ensureImage(ctx, c, st, req.Spec.Image, req.Pull); err != nil {
			return err
		}
		id, err := s.create(ctx, c, req.Spec.Name, body)
		if err != nil {
			return err
		}
		st.LogID(id, "Copy created %s", short(id))
		if req.Start {
			return c.JSON(ctx, http.MethodPost, "/containers/"+id+"/start", nil, nil, nil)
		}
		return nil
	}())
}

// ---- logs & stats ----

type logLine struct {
	T string `json:"t,omitempty"` // timestamp
	S string `json:"s"`           // out | err
	M string `json:"m"`
	C string `json:"c,omitempty"` // container (stack logs)
}

func (s *Server) containerLogs(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	id := r.PathValue("id")
	in, err := s.inspectRaw(r.Context(), c, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	st.Close(streamLogs(r.Context(), c, in.ID, get[bool](in.Config, "Tty"), logQuery(r), "", st))
}

func logQuery(r *http.Request) url.Values {
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}}
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "500"
	}
	q.Set("tail", tail)
	if r.URL.Query().Get("follow") != "0" {
		q.Set("follow", "1")
	}
	if since := r.URL.Query().Get("since"); since != "" {
		q.Set("since", since)
	}
	return q
}

func streamLogs(ctx context.Context, c *docker.Client, id string, tty bool, q url.Values, label string, st *stream) error {
	return streamLogsAt(ctx, c, "/containers/"+id+"/logs", tty, q, label, st)
}

// streamLogsAt streams a container or service log endpoint as logLine events.
func streamLogsAt(ctx context.Context, c *docker.Client, p string, tty bool, q url.Values, label string, st *stream) error {
	resp, err := c.Do(ctx, http.MethodGet, p, q, nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	emit := func(stream string) func([]byte) error {
		var pending []byte
		return func(p []byte) error {
			pending = append(pending, p...)
			for {
				i := indexByte(pending, '\n')
				if i < 0 {
					return nil
				}
				line := strings.TrimRight(string(pending[:i]), "\r")
				pending = pending[i+1:]
				ts, msg, ok := strings.Cut(line, " ")
				if !ok || !strings.Contains(ts, "T") {
					ts, msg = "", line
				}
				if err := st.Send(logLine{T: ts, S: stream, M: msg, C: label}); err != nil {
					return err
				}
			}
		}
	}
	if tty {
		out := emit("out")
		br := bufio.NewReaderSize(resp.Body, 32<<10)
		buf := make([]byte, 32<<10)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				if e := out(buf[:n]); e != nil {
					return e
				}
			}
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
		}
	}
	out, errs := emit("out"), emit("err")
	err = docker.Demux(resp.Body, func(stream byte, p []byte) error {
		if stream == 2 {
			return errs(p)
		}
		return out(p)
	})
	if err == io.EOF {
		return nil
	}
	return err
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

type statsSample struct {
	Read     string  `json:"read"`
	CPU      float64 `json:"cpu"` // percent of one CPU * ncpu
	NCPU     int     `json:"ncpu"`
	Mem      uint64  `json:"mem"`
	MemLimit uint64  `json:"mem_limit"`
	NetRx    uint64  `json:"net_rx"`
	NetTx    uint64  `json:"net_tx"`
	BlkRead  uint64  `json:"blk_read"`
	BlkWrite uint64  `json:"blk_write"`
	PIDs     uint64  `json:"pids"`
}

type rawStats struct {
	Read     string `json:"read"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	PidsStats struct {
		Current uint64 `json:"current"`
	} `json:"pids_stats"`
}

func (rs *rawStats) sample() statsSample {
	out := statsSample{Read: rs.Read, NCPU: rs.CPUStats.OnlineCPUs, MemLimit: rs.MemoryStats.Limit, PIDs: rs.PidsStats.Current}
	cpuDelta := float64(rs.CPUStats.CPUUsage.TotalUsage) - float64(rs.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(rs.CPUStats.SystemUsage) - float64(rs.PreCPUStats.SystemUsage)
	if sysDelta > 0 && cpuDelta > 0 {
		n := rs.CPUStats.OnlineCPUs
		if n == 0 {
			n = 1
		}
		out.CPU = cpuDelta / sysDelta * float64(n) * 100
	}
	out.Mem = rs.MemoryStats.Usage
	if v, ok := rs.MemoryStats.Stats["inactive_file"]; ok && v < out.Mem {
		out.Mem -= v
	} else if v, ok := rs.MemoryStats.Stats["cache"]; ok && v < out.Mem {
		out.Mem -= v
	}
	for _, n := range rs.Networks {
		out.NetRx += n.RxBytes
		out.NetTx += n.TxBytes
	}
	for _, b := range rs.BlkioStats.IOServiceBytesRecursive {
		switch strings.ToLower(b.Op) {
		case "read":
			out.BlkRead += b.Value
		case "write":
			out.BlkWrite += b.Value
		}
	}
	return out
}

func (s *Server) containerStats(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	stream := r.URL.Query().Get("stream") != "0"
	q := url.Values{"stream": {strconv.FormatBool(stream)}}
	if !stream {
		q.Set("one-shot", "0") // need precpu for a meaningful CPU value
	}
	resp, err := c.Do(r.Context(), http.MethodGet, "/containers/"+url.PathEscape(r.PathValue("id"))+"/stats", q, nil, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer resp.Body.Close()
	if !stream {
		var rs rawStats
		if err := json.NewDecoder(resp.Body).Decode(&rs); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, rs.sample())
		return
	}
	st := newStream(w)
	dec := json.NewDecoder(resp.Body)
	for {
		var rs rawStats
		if err := dec.Decode(&rs); err != nil {
			if err == io.EOF {
				err = nil
			}
			st.Close(err)
			return
		}
		if err := st.Send(rs.sample()); err != nil {
			st.Close(nil)
			return
		}
		st.flush()
	}
}

// ---- files ----

type fileEntry struct {
	Name  string `json:"name"`
	Dir   bool   `json:"dir"`
	Link  string `json:"link,omitempty"`
	Size  int64  `json:"size"`
	Mode  string `json:"mode"`
	MTime string `json:"mtime,omitempty"`
}

// containerFiles lists a directory by reading the tar stream of the
// archive API, so it works in images without a shell. Only the first level
// is kept and the stream stops after a size budget.
func (s *Server) containerFiles(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	dir := path.Clean("/" + r.URL.Query().Get("path"))
	resp, err := c.Do(r.Context(), http.MethodGet, "/containers/"+url.PathEscape(r.PathValue("id"))+"/archive", url.Values{"path": {dir + "/."}}, nil, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer resp.Body.Close()
	tr := tar.NewReader(io.LimitReader(resp.Body, 256<<20))
	entries := []fileEntry{}
	truncated := false
	for i := 0; ; i++ {
		h, err := tr.Next()
		if err != nil {
			if err != io.EOF {
				truncated = true
			}
			break
		}
		if i > 20000 {
			truncated = true
			break
		}
		name := strings.TrimPrefix(strings.TrimPrefix(h.Name, "./"), "/")
		name = strings.TrimSuffix(name, "/")
		if name == "" || name == "." || strings.Contains(name, "/") {
			continue
		}
		e := fileEntry{Name: name, Dir: h.Typeflag == tar.TypeDir, Size: h.Size, Mode: h.FileInfo().Mode().String(), MTime: h.ModTime.UTC().Format(time.RFC3339)}
		if h.Typeflag == tar.TypeSymlink {
			e.Link = h.Linkname
		}
		entries = append(entries, e)
	}
	writeJSON(w, map[string]any{"path": dir, "entries": entries, "truncated": truncated})
}

// containerDownload streams a file (raw) or a directory (tar).
func (s *Server) containerDownload(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	p := path.Clean("/" + r.URL.Query().Get("path"))
	resp, err := c.Do(r.Context(), http.MethodGet, "/containers/"+url.PathEscape(r.PathValue("id"))+"/archive", url.Values{"path": {p}}, nil, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer resp.Body.Close()
	base := path.Base(p)
	if r.URL.Query().Get("tar") == "1" {
		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, base+".tar"))
		io.Copy(w, resp.Body)
		return
	}
	tr := tar.NewReader(resp.Body)
	h, err := tr.Next()
	if err != nil {
		writeErr(w, err)
		return
	}
	if h.Typeflag != tar.TypeReg {
		writeErr(w, badRequest("not a regular file; download it as tar"))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(h.Size, 10))
	if r.URL.Query().Get("inline") == "1" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, base))
	}
	io.Copy(w, tr)
}
