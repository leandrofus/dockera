package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type ctNet struct {
	ID              string `json:"Id"`
	Names           []string
	State           string
	Mounts          []struct{ Type, Name, Destination string }
	NetworkSettings struct {
		Networks map[string]struct {
			NetworkID  string `json:"NetworkID"`
			IPAddress  string `json:"IPAddress"`
			MacAddress string `json:"MacAddress"`
		}
	}
}

type attached struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
	IPv4  string `json:"ipv4,omitempty"`
	Mac   string `json:"mac,omitempty"`
	Dest  string `json:"dest,omitempty"`
}

// listNetworks returns networks with the containers attached to each one.
func (s *Server) listNetworks(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var nets []map[string]any
	if err := c.Get(r.Context(), "/networks", nil, &nets); err != nil {
		writeErr(w, err)
		return
	}
	var cts []ctNet
	c.Get(r.Context(), "/containers/json", url.Values{"all": {"1"}}, &cts)
	byNet := map[string][]attached{}
	for _, ct := range cts {
		for _, n := range ct.NetworkSettings.Networks {
			byNet[n.NetworkID] = append(byNet[n.NetworkID], attached{ID: ct.ID, Name: name(ct.Names), State: ct.State, IPv4: n.IPAddress, Mac: n.MacAddress})
		}
	}
	for _, n := range nets {
		id, _ := n["Id"].(string)
		list := byNet[id]
		if list == nil {
			list = []attached{}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		n["Attached"] = list
	}
	sort.Slice(nets, func(i, j int) bool { return nets[i]["Name"].(string) < nets[j]["Name"].(string) })
	writeJSON(w, nets)
}

func name(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func (s *Server) inspectNetwork(w http.ResponseWriter, r *http.Request) {
	s.proxyJSON(w, r, "/networks/"+url.PathEscape(r.PathValue("id")), url.Values{"verbose": {"false"}})
}

func (s *Server) createNetwork(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Name       string            `json:"name"`
		Driver     string            `json:"driver"`
		Subnet     string            `json:"subnet"`
		Gateway    string            `json:"gateway"`
		IPRange    string            `json:"ip_range"`
		Internal   bool              `json:"internal"`
		Attachable bool              `json:"attachable"`
		IPv6       bool              `json:"ipv6"`
		Options    map[string]string `json:"options"`
		Labels     map[string]string `json:"labels"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	req := map[string]any{
		"Name": body.Name, "Driver": body.Driver, "Internal": body.Internal,
		"Attachable": body.Attachable, "EnableIPv6": body.IPv6, "Options": body.Options, "Labels": body.Labels,
	}
	if body.Subnet != "" {
		cfg := map[string]string{"Subnet": body.Subnet}
		if body.Gateway != "" {
			cfg["Gateway"] = body.Gateway
		}
		if body.IPRange != "" {
			cfg["IPRange"] = body.IPRange
		}
		req["IPAM"] = map[string]any{"Driver": "default", "Config": []any{cfg}}
	}
	var out json.RawMessage
	if err := c.JSON(r.Context(), http.MethodPost, "/networks/create", nil, req, &out); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

func (s *Server) removeNetwork(w http.ResponseWriter, r *http.Request) {
	s.simple(w, r, http.MethodDelete, "/networks/"+url.PathEscape(r.PathValue("id")), nil, nil)
}

func (s *Server) connectNetwork(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Container string   `json:"container"`
		Aliases   []string `json:"aliases"`
		IPv4      string   `json:"ipv4"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	ep := map[string]any{"Aliases": nilIfEmpty(body.Aliases)}
	if body.IPv4 != "" {
		ep["IPAMConfig"] = map[string]string{"IPv4Address": body.IPv4}
	}
	s.simple(w, r, http.MethodPost, "/networks/"+url.PathEscape(r.PathValue("id"))+"/connect", nil,
		map[string]any{"Container": body.Container, "EndpointConfig": ep})
}

func (s *Server) disconnectNetwork(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Container string `json:"container"`
		Force     bool   `json:"force"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	s.simple(w, r, http.MethodPost, "/networks/"+url.PathEscape(r.PathValue("id"))+"/disconnect", nil,
		map[string]any{"Container": body.Container, "Force": body.Force})
}

func (s *Server) pruneNetworks(w http.ResponseWriter, r *http.Request) {
	s.prune(w, r, "/networks/prune", nil)
}

// simple performs a request and answers {"ok":true}.
func (s *Server) simple(w http.ResponseWriter, r *http.Request, method, p string, q url.Values, body any) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := c.JSON(r.Context(), method, p, q, body, nil); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- volumes ----

func (s *Server) listVolumes(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var out struct {
		Volumes []map[string]any
	}
	if err := c.Get(r.Context(), "/volumes", nil, &out); err != nil {
		writeErr(w, err)
		return
	}
	var cts []ctNet
	c.Get(r.Context(), "/containers/json", url.Values{"all": {"1"}}, &cts)
	usedBy := map[string][]attached{}
	for _, ct := range cts {
		for _, m := range ct.Mounts {
			if m.Type == "volume" {
				usedBy[m.Name] = append(usedBy[m.Name], attached{ID: ct.ID, Name: name(ct.Names), State: ct.State, Dest: m.Destination})
			}
		}
	}
	sizes := map[string]float64{}
	if r.URL.Query().Get("size") == "1" {
		var df struct {
			Volumes []struct {
				Name      string
				UsageData struct{ Size float64 }
			}
		}
		if c.Get(r.Context(), "/system/df", url.Values{"type": {"volume"}}, &df) == nil {
			for _, v := range df.Volumes {
				sizes[v.Name] = v.UsageData.Size
			}
		}
	}
	vols := out.Volumes
	if vols == nil {
		vols = []map[string]any{}
	}
	for _, v := range vols {
		n, _ := v["Name"].(string)
		list := usedBy[n]
		if list == nil {
			list = []attached{}
		}
		v["UsedBy"] = list
		if sz, ok := sizes[n]; ok {
			v["Size"] = sz
		}
	}
	sort.Slice(vols, func(i, j int) bool { return vols[i]["Name"].(string) < vols[j]["Name"].(string) })
	writeJSON(w, vols)
}

func (s *Server) inspectVolume(w http.ResponseWriter, r *http.Request) {
	s.proxyJSON(w, r, "/volumes/"+url.PathEscape(r.PathValue("name")), nil)
}

func (s *Server) createVolume(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Name    string            `json:"name"`
		Driver  string            `json:"driver"`
		Options map[string]string `json:"options"`
		Labels  map[string]string `json:"labels"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	var out json.RawMessage
	if err := c.JSON(r.Context(), http.MethodPost, "/volumes/create", nil, map[string]any{
		"Name": body.Name, "Driver": body.Driver, "DriverOpts": body.Options, "Labels": body.Labels,
	}, &out); err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

func (s *Server) removeVolume(w http.ResponseWriter, r *http.Request) {
	s.simple(w, r, http.MethodDelete, "/volumes/"+url.PathEscape(r.PathValue("name")), url.Values{"force": {r.URL.Query().Get("force")}}, nil)
}

func (s *Server) pruneVolumes(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	if r.URL.Query().Get("all") == "1" {
		q.Set("filters", `{"all":["true"]}`)
	}
	s.prune(w, r, "/volumes/prune", q)
}

// ---- system ----

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var info, version json.RawMessage
	if err := c.Get(r.Context(), "/info", nil, &info); err != nil {
		writeErr(w, err)
		return
	}
	c.Get(r.Context(), "/version", nil, &version)
	writeJSON(w, map[string]any{"info": info, "version": version})
}

func (s *Server) diskUsage(w http.ResponseWriter, r *http.Request) {
	s.proxyJSON(w, r, "/system/df", nil)
}

func (s *Server) systemPrune(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Containers bool `json:"containers"`
		Images     bool `json:"images"`
		AllImages  bool `json:"all_images"`
		Networks   bool `json:"networks"`
		Volumes    bool `json:"volumes"`
		BuildCache bool `json:"build_cache"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	res := map[string]json.RawMessage{}
	run := func(key, p string, q url.Values) {
		var out json.RawMessage
		if err := c.JSON(ctx, http.MethodPost, p, q, nil, &out); err != nil {
			out, _ = json.Marshal(map[string]string{"error": err.Error()})
		}
		res[key] = out
	}
	if body.Containers {
		run("containers", "/containers/prune", nil)
	}
	if body.Images {
		q := url.Values{}
		if body.AllImages {
			q.Set("filters", `{"dangling":["false"]}`)
		}
		run("images", "/images/prune", q)
	}
	if body.Networks {
		run("networks", "/networks/prune", nil)
	}
	if body.Volumes {
		run("volumes", "/volumes/prune", url.Values{"filters": {`{"all":["true"]}`}})
	}
	if body.BuildCache {
		run("build_cache", "/build/prune", nil)
	}
	writeJSON(w, res)
}

// events streams the daemon event feed as NDJSON.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	q := url.Values{}
	if since := r.URL.Query().Get("since"); since != "" {
		q.Set("since", since)
	}
	if f := r.URL.Query().Get("filters"); f != "" {
		q.Set("filters", f)
	}
	if until := r.URL.Query().Get("until"); until != "" {
		q.Set("until", until)
	}
	resp, err := c.Do(r.Context(), http.MethodGet, "/events", q, nil, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer resp.Body.Close()
	st := newStream(w)
	dec := json.NewDecoder(resp.Body)
	for {
		var ev json.RawMessage
		if err := dec.Decode(&ev); err != nil {
			st.Close(nil)
			return
		}
		if err := st.Raw(ev); err != nil {
			st.Close(nil)
			return
		}
		st.flush()
	}
}
