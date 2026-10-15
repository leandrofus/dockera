package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ducktainer/internal/compose"
	"ducktainer/internal/docker"
	"ducktainer/internal/i18n"
)

// Swarm mode: cluster, nodes, services, tasks, secrets, configs and stacks
// deployed with `docker stack deploy`. Everything goes through the Engine
// API except stacks, which need the docker CLI (as compose stacks do).

const lblStack = "com.docker.stack.namespace"

// ---- cluster ----

type swarmInfo struct {
	NodeID           string `json:"NodeID"`
	NodeAddr         string `json:"NodeAddr"`
	LocalNodeState   string `json:"LocalNodeState"`
	ControlAvailable bool   `json:"ControlAvailable"`
	Error            string `json:"Error"`
	RemoteManagers   []struct {
		NodeID string `json:"NodeID"`
		Addr   string `json:"Addr"`
	} `json:"RemoteManagers"`
	Nodes    int `json:"Nodes"`
	Managers int `json:"Managers"`
}

// swarmState reads the node's view of the cluster from /info.
func swarmState(ctx context.Context, c *docker.Client) (*swarmInfo, error) {
	var info struct{ Swarm swarmInfo }
	if err := c.Get(ctx, "/info", nil, &info); err != nil {
		return nil, err
	}
	return &info.Swarm, nil
}

func (s *Server) swarmInfo(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	st, err := swarmState(r.Context(), c)
	if err != nil {
		writeErr(w, err)
		return
	}
	resp := map[string]any{"info": st}
	if st.ControlAvailable {
		var sw map[string]any
		if err := c.Get(r.Context(), "/swarm", nil, &sw); err == nil {
			delete(sw, "JoinTokens") // admins read them from /swarm/tokens
			resp["cluster"] = sw
		}
	}
	writeJSON(w, resp)
}

func (s *Server) swarmTokens(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var sw struct {
		JoinTokens struct{ Worker, Manager string }
	}
	if err := c.Get(r.Context(), "/swarm", nil, &sw); err != nil {
		writeErr(w, err)
		return
	}
	st, _ := swarmState(r.Context(), c)
	addr := ""
	if st != nil {
		addr = st.NodeAddr
		for _, m := range st.RemoteManagers {
			if m.NodeID == st.NodeID {
				addr = m.Addr
			}
		}
	}
	writeJSON(w, map[string]string{"worker": sw.JoinTokens.Worker, "manager": sw.JoinTokens.Manager, "addr": addr})
}

func (s *Server) swarmInit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AdvertiseAddr string   `json:"advertise_addr"`
		ListenAddr    string   `json:"listen_addr"`
		DataPathAddr  string   `json:"data_path_addr"`
		AddrPool      []string `json:"default_addr_pool"`
		ForceNew      bool     `json:"force_new_cluster"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if body.ListenAddr == "" {
		body.ListenAddr = "0.0.0.0:2377"
	}
	req := map[string]any{"ListenAddr": body.ListenAddr, "AdvertiseAddr": body.AdvertiseAddr, "ForceNewCluster": body.ForceNew}
	if body.DataPathAddr != "" {
		req["DataPathAddr"] = body.DataPathAddr
	}
	if len(body.AddrPool) > 0 {
		req["DefaultAddrPool"] = body.AddrPool
	}
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var id string
	if err := c.JSON(r.Context(), http.MethodPost, "/swarm/init", nil, req, &id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"node_id": id})
}

func (s *Server) swarmJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Remote        string `json:"remote"`
		Token         string `json:"token"`
		AdvertiseAddr string `json:"advertise_addr"`
		ListenAddr    string `json:"listen_addr"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if body.Remote == "" || body.Token == "" {
		writeErr(w, badRequest("the manager address and the join token are required"))
		return
	}
	if body.ListenAddr == "" {
		body.ListenAddr = "0.0.0.0:2377"
	}
	remote := body.Remote
	if !strings.Contains(remote, ":") {
		remote += ":2377"
	}
	s.simple(w, r, http.MethodPost, "/swarm/join", nil, map[string]any{
		"ListenAddr": body.ListenAddr, "AdvertiseAddr": body.AdvertiseAddr, "RemoteAddrs": []string{remote}, "JoinToken": body.Token,
	})
}

func (s *Server) swarmLeave(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	if r.URL.Query().Get("force") == "1" {
		q.Set("force", "1")
	}
	s.simple(w, r, http.MethodPost, "/swarm/leave", q, nil)
}

// swarmRotate issues a new worker or manager join token.
func (s *Server) swarmRotate(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var sw struct {
		Version struct{ Index uint64 }
		Spec    json.RawMessage
	}
	if err := c.Get(r.Context(), "/swarm", nil, &sw); err != nil {
		writeErr(w, err)
		return
	}
	q := url.Values{"version": {strconv.FormatUint(sw.Version.Index, 10)}}
	switch r.PathValue("which") {
	case "worker":
		q.Set("rotateWorkerToken", "true")
	case "manager":
		q.Set("rotateManagerToken", "true")
	default:
		writeErr(w, badRequest("unknown token"))
		return
	}
	if err := c.JSON(r.Context(), http.MethodPost, "/swarm/update", q, []byte(sw.Spec), nil); err != nil {
		writeErr(w, err)
		return
	}
	s.swarmTokens(w, r)
}

// ---- nodes ----

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var nodes []map[string]any
	if err := c.Get(r.Context(), "/nodes", nil, &nodes); err != nil {
		writeErr(w, err)
		return
	}
	// running tasks per node
	var tasks []rawTask
	c.Get(r.Context(), "/tasks", url.Values{"filters": {`{"desired-state":["running"]}`}}, &tasks)
	per := map[string]int{}
	for _, t := range tasks {
		if t.Status.State == "running" {
			per[t.NodeID]++
		}
	}
	for _, n := range nodes {
		id, _ := n["ID"].(string)
		n["RunningTasks"] = per[id]
	}
	if nodes == nil {
		nodes = []map[string]any{}
	}
	writeJSON(w, nodes)
}

func (s *Server) inspectNode(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	id := r.PathValue("id")
	var node json.RawMessage
	if err := c.Get(r.Context(), "/nodes/"+url.PathEscape(id), nil, &node); err != nil {
		writeErr(w, err)
		return
	}
	tasks, err := s.tasks(r.Context(), c, "node", id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"node": node, "tasks": tasks})
}

func (s *Server) updateNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Availability string             `json:"availability"`
		Role         string             `json:"role"`
		Name         *string            `json:"name"`
		Labels       *map[string]string `json:"labels"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	switch body.Availability {
	case "", "active", "pause", "drain":
	default:
		writeErr(w, badRequest("unknown availability %q", body.Availability))
		return
	}
	switch body.Role {
	case "", "worker", "manager":
	default:
		writeErr(w, badRequest("unknown role %q", body.Role))
		return
	}
	id := url.PathEscape(r.PathValue("id"))
	err = retrySequence(func() error {
		var node struct {
			Version struct{ Index uint64 }
			Spec    map[string]any
		}
		if err := c.Get(r.Context(), "/nodes/"+id, nil, &node); err != nil {
			return err
		}
		if body.Availability != "" {
			node.Spec["Availability"] = body.Availability
		}
		if body.Role != "" {
			node.Spec["Role"] = body.Role
		}
		if body.Name != nil {
			node.Spec["Name"] = *body.Name
		}
		if body.Labels != nil {
			node.Spec["Labels"] = *body.Labels
		}
		return c.JSON(r.Context(), http.MethodPost, "/nodes/"+id+"/update", url.Values{"version": {strconv.FormatUint(node.Version.Index, 10)}}, node.Spec, nil)
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) removeNode(w http.ResponseWriter, r *http.Request) {
	s.simple(w, r, http.MethodDelete, "/nodes/"+url.PathEscape(r.PathValue("id")), url.Values{"force": {boolParam(r, "force")}}, nil)
}

func boolParam(r *http.Request, k string) string {
	if r.URL.Query().Get(k) == "1" {
		return "true"
	}
	return "false"
}

// ---- tasks ----

type rawTask struct {
	ID           string `json:"ID"`
	ServiceID    string `json:"ServiceID"`
	NodeID       string `json:"NodeID"`
	Slot         int    `json:"Slot"`
	DesiredState string `json:"DesiredState"`
	CreatedAt    string `json:"CreatedAt"`
	UpdatedAt    string `json:"UpdatedAt"`
	Spec         struct {
		ContainerSpec struct{ Image string } `json:"ContainerSpec"`
	} `json:"Spec"`
	Status struct {
		Timestamp       string `json:"Timestamp"`
		State           string `json:"State"`
		Message         string `json:"Message"`
		Err             string `json:"Err"`
		ContainerStatus struct {
			ContainerID string `json:"ContainerID"`
			ExitCode    int    `json:"ExitCode"`
		} `json:"ContainerStatus"`
	} `json:"Status"`
}

type task struct {
	ID           string `json:"id"`
	ServiceID    string `json:"service_id"`
	Service      string `json:"service"`
	NodeID       string `json:"node_id"`
	Node         string `json:"node"`
	Slot         int    `json:"slot"`
	Image        string `json:"image"`
	State        string `json:"state"`
	DesiredState string `json:"desired_state"`
	Message      string `json:"message"`
	Error        string `json:"error,omitempty"`
	ContainerID  string `json:"container_id,omitempty"`
	ExitCode     int    `json:"exit_code"`
	Updated      string `json:"updated"`
	Created      string `json:"created"`
}

// tasks lists tasks filtered by service or node with names resolved.
func (s *Server) tasks(ctx context.Context, c *docker.Client, key, value string) ([]task, error) {
	f, _ := json.Marshal(map[string][]string{key: {value}})
	var raw []rawTask
	if err := c.Get(ctx, "/tasks", url.Values{"filters": {string(f)}}, &raw); err != nil {
		return nil, err
	}
	names := s.swarmNames(ctx, c)
	out := make([]task, 0, len(raw))
	for _, t := range raw {
		out = append(out, task{
			ID: t.ID, ServiceID: t.ServiceID, Service: names.services[t.ServiceID], NodeID: t.NodeID, Node: names.nodes[t.NodeID],
			Slot: t.Slot, Image: t.Spec.ContainerSpec.Image, State: t.Status.State, DesiredState: t.DesiredState,
			Message: t.Status.Message, Error: t.Status.Err, ContainerID: t.Status.ContainerStatus.ContainerID,
			ExitCode: t.Status.ContainerStatus.ExitCode, Updated: t.Status.Timestamp, Created: t.CreatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out, nil
}

type swarmNames struct{ nodes, services map[string]string }

func (s *Server) swarmNames(ctx context.Context, c *docker.Client) swarmNames {
	n := swarmNames{nodes: map[string]string{}, services: map[string]string{}}
	var wg sync.WaitGroup
	wg.Go(func() {
		var nodes []struct {
			ID          string
			Description struct{ Hostname string }
		}
		c.Get(ctx, "/nodes", nil, &nodes)
		for _, x := range nodes {
			n.nodes[x.ID] = x.Description.Hostname
		}
	})
	wg.Go(func() {
		var svcs []struct {
			ID   string
			Spec struct{ Name string }
		}
		c.Get(ctx, "/services", nil, &svcs)
		for _, x := range svcs {
			n.services[x.ID] = x.Spec.Name
		}
	})
	wg.Wait()
	return n
}

// ---- services ----

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var svcs []map[string]any
	if err := c.Get(r.Context(), "/services", url.Values{"status": {"true"}}, &svcs); err != nil {
		writeErr(w, err)
		return
	}
	if svcs == nil {
		svcs = []map[string]any{}
	}
	writeJSON(w, svcs)
}

func (s *Server) inspectService(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	id := r.PathValue("id")
	var svc map[string]any
	if err := c.Get(r.Context(), "/services/"+url.PathEscape(id), url.Values{"insertDefaults": {"true"}}, &svc); err != nil {
		writeErr(w, err)
		return
	}
	sid, _ := svc["ID"].(string)
	// task counts only come with the list endpoint
	var status []struct {
		ID            string
		ServiceStatus json.RawMessage
	}
	f, _ := json.Marshal(map[string][]string{"id": {sid}})
	if c.Get(r.Context(), "/services", url.Values{"status": {"true"}, "filters": {string(f)}}, &status) == nil {
		for _, x := range status {
			if x.ID == sid {
				svc["ServiceStatus"] = x.ServiceStatus
			}
		}
	}
	tasks, err := s.tasks(r.Context(), c, "service", sid)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"service": svc, "tasks": tasks})
}

// serviceImage is the image of a spec, for registry credentials.
func serviceImage(spec map[string]any) string {
	tt, _ := spec["TaskTemplate"].(map[string]any)
	cs, _ := tt["ContainerSpec"].(map[string]any)
	img, _ := cs["Image"].(string)
	return img
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	var spec map[string]any
	if err := readJSON(r, &spec); err != nil {
		writeErr(w, err)
		return
	}
	if serviceImage(spec) == "" {
		writeErr(w, badRequest("the service needs an image"))
		return
	}
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var out json.RawMessage
	resp, err := c.Do(r.Context(), http.MethodPost, "/services/create", nil, spec, http.Header{"X-Registry-Auth": {s.registryAuth(serviceImage(spec))}})
	if err != nil {
		writeErr(w, err)
		return
	}
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&out)
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// currentService returns a service's spec and version.
func currentService(ctx context.Context, c *docker.Client, id string) (map[string]any, uint64, map[string]any, error) {
	var svc struct {
		ID           string
		Version      struct{ Index uint64 }
		Spec         map[string]any
		PreviousSpec map[string]any
	}
	if err := c.Get(ctx, "/services/"+url.PathEscape(id), nil, &svc); err != nil {
		return nil, 0, nil, err
	}
	return svc.Spec, svc.Version.Index, svc.PreviousSpec, nil
}

// pushService sends an updated spec (rollback: revert to the previous one).
func (s *Server) pushService(ctx context.Context, c *docker.Client, id string, version uint64, spec map[string]any, rollback bool) ([]string, error) {
	q := url.Values{"version": {strconv.FormatUint(version, 10)}}
	if rollback {
		q.Set("rollback", "previous")
	}
	var out struct{ Warnings []string }
	resp, err := c.Do(ctx, http.MethodPost, "/services/"+url.PathEscape(id)+"/update", q, spec, http.Header{"X-Registry-Auth": {s.registryAuth(serviceImage(spec))}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(&out)
	return out.Warnings, nil
}

// updateService replaces a service's spec (the advanced JSON editor).
func (s *Server) updateService(w http.ResponseWriter, r *http.Request) {
	var spec map[string]any
	if err := readJSON(r, &spec); err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	id := r.PathValue("id")
	var warnings []string
	err = retrySequence(func() error {
		_, version, _, err := currentService(r.Context(), c, id)
		if err != nil {
			return err
		}
		warnings, err = s.pushService(r.Context(), c, id, version, spec, false)
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "warnings": warnings})
}

// retrySequence repeats a read-modify-write of a Swarm object while the
// manager reports that its version moved in between (an update in flight
// changes it too).
func retrySequence(fn func() error) error {
	var err error
	for range 5 {
		if err = fn(); err == nil || !strings.Contains(err.Error(), "out of sequence") {
			return err
		}
		time.Sleep(300 * time.Millisecond)
	}
	return err
}

// serviceAction applies a quick change: scale, restart (force update),
// rollback or a new image.
func (s *Server) serviceAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Replicas *uint64 `json:"replicas"`
		Image    string  `json:"image"`
	}
	if r.ContentLength != 0 {
		if err := readJSON(r, &body); err != nil {
			writeErr(w, err)
			return
		}
	}
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	id, action := r.PathValue("id"), r.PathValue("action")
	var warnings []string
	err = retrySequence(func() error {
		spec, version, prev, err := currentService(r.Context(), c, id)
		if err != nil {
			return err
		}
		rollback, err := applyServiceAction(spec, prev, action, body.Replicas, body.Image)
		if err != nil {
			return err
		}
		warnings, err = s.pushService(r.Context(), c, id, version, spec, rollback)
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "warnings": warnings})
}

// applyServiceAction edits a spec for a quick action; rollback asks the
// manager to revert to the previous spec instead.
func applyServiceAction(spec, prev map[string]any, action string, replicas *uint64, image string) (rollback bool, err error) {
	tt, _ := spec["TaskTemplate"].(map[string]any)
	if tt == nil {
		tt = map[string]any{}
		spec["TaskTemplate"] = tt
	}
	bump := func() {
		n, _ := tt["ForceUpdate"].(float64)
		tt["ForceUpdate"] = n + 1
	}
	switch action {
	case "scale":
		mode, _ := spec["Mode"].(map[string]any)
		rep, _ := mode["Replicated"].(map[string]any)
		if rep == nil || replicas == nil {
			return false, badRequest("only replicated services can be scaled")
		}
		rep["Replicas"] = *replicas
	case "restart":
		bump()
	case "rollback":
		if prev == nil {
			return false, badRequest("the service has no previous version to roll back to")
		}
		return true, nil
	case "image":
		cs, _ := tt["ContainerSpec"].(map[string]any)
		if cs == nil || image == "" {
			return false, badRequest("the service needs an image")
		}
		cs["Image"] = image
		bump() // re-resolve the tag even when it did not change
	default:
		return false, badRequest("unknown action")
	}
	return false, nil
}

func (s *Server) removeService(w http.ResponseWriter, r *http.Request) {
	s.simple(w, r, http.MethodDelete, "/services/"+url.PathEscape(r.PathValue("id")), nil, nil)
}

func (s *Server) serviceLogs(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	id := r.PathValue("id")
	spec, _, _, err := currentService(r.Context(), c, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	st.Close(streamLogsAt(r.Context(), c, "/services/"+url.PathEscape(id)+"/logs", serviceTTY(spec), logQuery(r), "", st))
}

func serviceTTY(spec map[string]any) bool {
	tt, _ := spec["TaskTemplate"].(map[string]any)
	cs, _ := tt["ContainerSpec"].(map[string]any)
	tty, _ := cs["TTY"].(bool)
	return tty
}

// ---- secrets and configs ----

// swarmObject handles the shared shape of /secrets and /configs.
type swarmObject string

func (k swarmObject) list(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := s.ep(r)
		if err != nil {
			writeErr(w, err)
			return
		}
		var list []map[string]any
		if err := c.Get(r.Context(), "/"+string(k), nil, &list); err != nil {
			writeErr(w, err)
			return
		}
		// which services use each one
		var svcs []struct {
			ID   string
			Spec struct {
				Name         string
				TaskTemplate struct {
					ContainerSpec struct {
						Secrets []struct{ SecretID string }
						Configs []struct{ ConfigID string }
					}
				}
			}
		}
		c.Get(r.Context(), "/services", nil, &svcs)
		used := map[string][]map[string]string{}
		for _, sv := range svcs {
			cs := sv.Spec.TaskTemplate.ContainerSpec
			var ids []string
			if k == "secrets" {
				for _, x := range cs.Secrets {
					ids = append(ids, x.SecretID)
				}
			} else {
				for _, x := range cs.Configs {
					ids = append(ids, x.ConfigID)
				}
			}
			for _, id := range ids {
				used[id] = append(used[id], map[string]string{"id": sv.ID, "name": sv.Spec.Name})
			}
		}
		for _, o := range list {
			id, _ := o["ID"].(string)
			u := used[id]
			if u == nil {
				u = []map[string]string{}
			}
			o["UsedBy"] = u
			if spec, ok := o["Spec"].(map[string]any); ok {
				delete(spec, "Data") // configs: shown on the record page only
			}
		}
		if list == nil {
			list = []map[string]any{}
		}
		writeJSON(w, list)
	}
}

func (k swarmObject) inspect(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.proxyJSON(w, r, "/"+string(k)+"/"+url.PathEscape(r.PathValue("id")), nil)
	}
}

func (k swarmObject) create(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name   string            `json:"name"`
			Data   string            `json:"data"`
			Base64 bool              `json:"base64"`
			Labels map[string]string `json:"labels"`
		}
		if err := readJSON(r, &body); err != nil {
			writeErr(w, err)
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			writeErr(w, badRequest("a name is required"))
			return
		}
		data := base64.StdEncoding.EncodeToString([]byte(body.Data))
		if body.Base64 {
			raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.Data))
			if err != nil {
				writeErr(w, badRequest("invalid base64: %v", err))
				return
			}
			data = base64.StdEncoding.EncodeToString(raw)
		}
		c, err := s.ep(r)
		if err != nil {
			writeErr(w, err)
			return
		}
		var out json.RawMessage
		if err := c.JSON(r.Context(), http.MethodPost, "/"+string(k)+"/create", nil, map[string]any{
			"Name": strings.TrimSpace(body.Name), "Data": data, "Labels": body.Labels,
		}, &out); err != nil {
			writeErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	}
}

func (k swarmObject) remove(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.simple(w, r, http.MethodDelete, "/"+string(k)+"/"+url.PathEscape(r.PathValue("id")), nil, nil)
	}
}

// ---- stacks (docker stack deploy) ----

type swarmService struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Mode    string `json:"mode"`
	Running int    `json:"running"`
	Desired int    `json:"desired"`
}

type swarmStack struct {
	Name     string         `json:"name"`
	Managed  bool           `json:"managed"`
	Services []swarmService `json:"services"`
	Running  int            `json:"running"`
	Desired  int            `json:"desired"`
	Status   string         `json:"status"` // running | partial | stopped | empty
}

func (s *Server) swarmStacks(ctx context.Context, c *docker.Client, ep string) (map[string]*swarmStack, error) {
	var svcs []struct {
		ID   string
		Spec struct {
			Name         string
			Labels       map[string]string
			Mode         map[string]any
			TaskTemplate struct{ ContainerSpec struct{ Image string } }
		}
		ServiceStatus struct{ RunningTasks, DesiredTasks int }
	}
	if err := c.Get(ctx, "/services", url.Values{"status": {"true"}, "filters": {`{"label":["` + lblStack + `"]}`}}, &svcs); err != nil {
		return nil, err
	}
	out := map[string]*swarmStack{}
	get := func(n string) *swarmStack {
		if out[n] == nil {
			out[n] = &swarmStack{Name: n, Services: []swarmService{}}
		}
		return out[n]
	}
	for _, sv := range svcs {
		st := get(sv.Spec.Labels[lblStack])
		mode := "replicated"
		for k := range sv.Spec.Mode {
			mode = strings.ToLower(k)
		}
		img, _, _ := strings.Cut(sv.Spec.TaskTemplate.ContainerSpec.Image, "@")
		st.Services = append(st.Services, swarmService{
			ID: sv.ID, Name: sv.Spec.Name, Image: img, Mode: mode,
			Running: sv.ServiceStatus.RunningTasks, Desired: sv.ServiceStatus.DesiredTasks,
		})
		st.Running += sv.ServiceStatus.RunningTasks
		st.Desired += sv.ServiceStatus.DesiredTasks
	}
	for _, n := range s.SwarmStacks.Managed(ep) {
		get(n).Managed = true
	}
	for _, st := range out {
		sort.Slice(st.Services, func(i, j int) bool { return st.Services[i].Name < st.Services[j].Name })
		switch {
		case len(st.Services) == 0:
			st.Status = "empty"
		case st.Running >= st.Desired && st.Desired > 0:
			st.Status = "running"
		case st.Running == 0:
			st.Status = "stopped"
		default:
			st.Status = "partial"
		}
	}
	return out, nil
}

func (s *Server) listSwarmStacks(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	m, err := s.swarmStacks(r.Context(), c, r.PathValue("ep"))
	if err != nil {
		writeErr(w, err)
		return
	}
	list := make([]*swarmStack, 0, len(m))
	for _, st := range m {
		list = append(list, st)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	writeJSON(w, list)
}

func (s *Server) swarmStack(r *http.Request) (*docker.Client, *swarmStack, error) {
	c, err := s.ep(r)
	if err != nil {
		return nil, nil, err
	}
	m, err := s.swarmStacks(r.Context(), c, r.PathValue("ep"))
	if err != nil {
		return nil, nil, err
	}
	st, ok := m[r.PathValue("name")]
	if !ok {
		return nil, nil, herr(http.StatusNotFound, "stack not found: %s", r.PathValue("name"))
	}
	return c, st, nil
}

func (s *Server) getSwarmStack(w http.ResponseWriter, r *http.Request) {
	_, st, err := s.swarmStack(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	resp := map[string]any{"stack": st}
	if st.Managed {
		if f, err := s.SwarmStacks.Read(r.PathValue("ep"), st.Name); err == nil {
			resp["files"] = f
		}
	}
	writeJSON(w, resp)
}

// stackCLI runs `docker stack` for a stored stack, streaming its output.
func (s *Server) stackCLI(ctx context.Context, st *stream, ep string, f *compose.Files, args ...string) error {
	rn, err := s.runner(ep)
	if err != nil {
		return err
	}
	lw := &lineWriter{s: st}
	defer lw.Flush()
	dir, vars := "", []string(nil)
	if f != nil {
		dir, vars = f.Dir, compose.ParseEnv(f.Env)
	}
	if err := rn.Stack(ctx, lw, lw, dir, vars, args...); err != nil {
		lw.Flush()
		if lw.last != "" && !strings.HasPrefix(lw.last, "$ ") {
			return fmt.Errorf("%s (%w)", lw.last, err)
		}
		return err
	}
	return nil
}

// checkSwarmStack validates a compose file for `docker stack deploy`.
func (s *Server) checkSwarmStack(ctx context.Context, ep string, f *compose.Files) error {
	rn, err := s.runner(ep)
	if err != nil {
		return err
	}
	var errb bytes.Buffer
	if err := rn.Stack(ctx, io.Discard, &errb, f.Dir, compose.ParseEnv(f.Env), "config", "-c", f.Files[0].Path); err != nil {
		lines := strings.Split(strings.TrimSpace(errb.String()), "\n")
		return fmt.Errorf("%s (%w)", lines[len(lines)-1], err)
	}
	return nil
}

func deployArgs(name string, f *compose.Files, prune bool) []string {
	args := []string{"deploy", "--with-registry-auth", "--detach=true", "-c", f.Files[0].Path}
	if prune {
		args = append(args, "--prune")
	}
	return append(args, name)
}

type swarmStackReq struct {
	Name    string `json:"name"`
	Compose string `json:"compose"`
	Env     string `json:"env"`
	Deploy  bool   `json:"deploy"`
	Prune   *bool  `json:"prune"`
}

func (s *Server) createSwarmStack(w http.ResponseWriter, r *http.Request) {
	ep := r.PathValue("ep")
	var req swarmStackReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	if err := compose.ValidName(req.Name); err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	if s.SwarmStacks.Exists(ep, req.Name) {
		writeErr(w, herr(http.StatusConflict, "a stack with that name already exists"))
		return
	}
	if _, err := s.SwarmStacks.Write(ep, req.Name, req.Compose, req.Env); err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	ctx := context.WithoutCancel(r.Context())
	st.Close(func() error {
		f, err := s.SwarmStacks.Read(ep, req.Name)
		if err != nil {
			return err
		}
		if err := s.checkSwarmStack(ctx, ep, f); err != nil {
			s.SwarmStacks.Delete(ep, req.Name)
			return i18n.Errorf("invalid compose, not saved: %v", err)
		}
		st.Log("Stack saved")
		if !req.Deploy {
			return nil
		}
		return s.stackCLI(ctx, st, ep, f, deployArgs(req.Name, f, true)...)
	}())
}

func (s *Server) updateSwarmStack(w http.ResponseWriter, r *http.Request) {
	ep, name := r.PathValue("ep"), r.PathValue("name")
	var req swarmStackReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if err := compose.ValidName(name); err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	// saving a stack deployed elsewhere adopts it
	prev, _ := s.SwarmStacks.Read(ep, name)
	if _, err := s.SwarmStacks.Write(ep, name, req.Compose, req.Env); err != nil {
		writeErr(w, err)
		return
	}
	restore := func() {
		if prev != nil {
			s.SwarmStacks.Write(ep, name, prev.Files[0].Content, prev.Env)
		} else {
			s.SwarmStacks.Delete(ep, name)
		}
	}
	st := newStream(w)
	ctx := context.WithoutCancel(r.Context())
	st.Close(func() error {
		f, err := s.SwarmStacks.Read(ep, name)
		if err != nil {
			return err
		}
		if err := s.checkSwarmStack(ctx, ep, f); err != nil {
			restore()
			return i18n.Errorf("invalid compose, the previous version was restored: %v", err)
		}
		st.Log("Changes saved")
		if !req.Deploy {
			return nil
		}
		return s.stackCLI(ctx, st, ep, f, deployArgs(name, f, req.Prune == nil || *req.Prune)...)
	}())
}

func (s *Server) deploySwarmStack(w http.ResponseWriter, r *http.Request) {
	ep, name := r.PathValue("ep"), r.PathValue("name")
	f, err := s.SwarmStacks.Read(ep, name)
	if err != nil {
		writeErr(w, badRequest("ducktainer has no compose file for %s: paste it in the Compose tab to manage it from here", name))
		return
	}
	st := newStream(w)
	st.Close(s.stackCLI(context.WithoutCancel(r.Context()), st, ep, f, deployArgs(name, f, true)...))
}

func (s *Server) removeSwarmStack(w http.ResponseWriter, r *http.Request) {
	ep, name := r.PathValue("ep"), r.PathValue("name")
	_, info, err := s.swarmStack(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	st.Close(func() error {
		ctx := context.WithoutCancel(r.Context())
		// also removes networks, secrets and configs left by a failed deploy
		if err := s.stackCLI(ctx, st, ep, nil, "rm", name); err != nil && (len(info.Services) > 0 || !strings.Contains(err.Error(), "Nothing found in stack")) {
			return err
		}
		if info.Managed && r.URL.Query().Get("keep") != "1" {
			if err := s.SwarmStacks.Delete(ep, name); err != nil {
				return err
			}
			st.Log("Stack files removed")
		}
		return nil
	}())
}

// swarmStackLogs merges the logs of every service in a stack.
func (s *Server) swarmStackLogs(w http.ResponseWriter, r *http.Request) {
	c, info, err := s.swarmStack(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	st := newStream(w)
	q := logQuery(r)
	svc := r.URL.Query().Get("service")
	var wg sync.WaitGroup
	for _, sv := range info.Services {
		if svc != "" && sv.Name != svc {
			continue
		}
		spec, _, _, err := currentService(r.Context(), c, sv.ID)
		if err != nil {
			continue
		}
		label := strings.TrimPrefix(sv.Name, info.Name+"_")
		wg.Go(func() {
			if err := streamLogsAt(r.Context(), c, "/services/"+sv.ID+"/logs", serviceTTY(spec), q, label, st); err != nil && r.Context().Err() == nil {
				b, _ := json.Marshal(logLine{S: "err", M: "[ducktainer] " + err.Error(), C: label})
				st.Raw(b)
			}
		})
	}
	wg.Wait()
	st.Close(nil)
}
