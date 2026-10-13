package server

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Spec is the editable view of a container. Settings it doesn't cover are
// preserved from the original container when recreating.
type Spec struct {
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Cmd         []string          `json:"cmd"`
	Entrypoint  []string          `json:"entrypoint"`
	Env         []string          `json:"env"`
	Ports       []PortSpec        `json:"ports"`
	Mounts      []MountSpec       `json:"mounts"`
	Restart     string            `json:"restart"`
	Labels      map[string]string `json:"labels"`
	Networks    []NetSpec         `json:"networks"`
	NetworkMode string            `json:"network_mode"`
	Hostname    string            `json:"hostname"`
	User        string            `json:"user"`
	WorkingDir  string            `json:"working_dir"`
	Privileged  bool              `json:"privileged"`
	Tty         bool              `json:"tty"`
	OpenStdin   bool              `json:"open_stdin"`
	Memory      int64             `json:"memory"`    // bytes, 0 = unlimited
	NanoCPUs    int64             `json:"nano_cpus"` // 1e9 = 1 CPU
	CapAdd      []string          `json:"cap_add"`
	ExtraHosts  []string          `json:"extra_hosts"`
}

type PortSpec struct {
	HostIP    string `json:"host_ip"`
	HostPort  string `json:"host_port"`
	Container string `json:"container"` // "80" or "80/udp"
}

type MountSpec struct {
	Type     string `json:"type"` // bind | volume | tmpfs
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"readonly"`
}

type NetSpec struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	IPv4    string   `json:"ipv4"`
}

// inspect is the subset of container inspect we read; Config and HostConfig
// are kept raw so that unknown fields survive a recreate.
type inspect struct {
	ID              string                     `json:"Id"`
	Name            string                     `json:"Name"`
	Config          map[string]json.RawMessage `json:"Config"`
	HostConfig      map[string]json.RawMessage `json:"HostConfig"`
	State           struct{ Running bool }     `json:"State"`
	Mounts          []rawMount                 `json:"Mounts"`
	NetworkSettings struct {
		Networks map[string]struct {
			Aliases    []string `json:"Aliases"`
			IPAMConfig *struct {
				IPv4Address string `json:"IPv4Address"`
			} `json:"IPAMConfig"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

type rawMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

func get[T any](m map[string]json.RawMessage, key string) T {
	var v T
	if raw, ok := m[key]; ok {
		_ = json.Unmarshal(raw, &v)
	}
	return v
}

func specFromInspect(in *inspect) Spec {
	sp := Spec{
		Name:       strings.TrimPrefix(in.Name, "/"),
		Image:      get[string](in.Config, "Image"),
		Cmd:        get[[]string](in.Config, "Cmd"),
		Entrypoint: get[[]string](in.Config, "Entrypoint"),
		Env:        get[[]string](in.Config, "Env"),
		Labels:     get[map[string]string](in.Config, "Labels"),
		Hostname:   get[string](in.Config, "Hostname"),
		User:       get[string](in.Config, "User"),
		WorkingDir: get[string](in.Config, "WorkingDir"),
		Tty:        get[bool](in.Config, "Tty"),
		OpenStdin:  get[bool](in.Config, "OpenStdin"),
		Privileged: get[bool](in.HostConfig, "Privileged"),
		Memory:     get[int64](in.HostConfig, "Memory"),
		NanoCPUs:   get[int64](in.HostConfig, "NanoCpus"),
		CapAdd:     get[[]string](in.HostConfig, "CapAdd"),
		ExtraHosts: get[[]string](in.HostConfig, "ExtraHosts"),
	}
	if len(in.ID) >= 12 && sp.Hostname == in.ID[:12] {
		sp.Hostname = "" // default hostname, let docker pick a new one
	}
	rp := get[struct {
		Name              string
		MaximumRetryCount int
	}](in.HostConfig, "RestartPolicy")
	sp.Restart = rp.Name
	if rp.Name == "on-failure" && rp.MaximumRetryCount > 0 {
		sp.Restart = fmt.Sprintf("on-failure:%d", rp.MaximumRetryCount)
	}
	bindings := get[map[string][]struct{ HostIp, HostPort string }](in.HostConfig, "PortBindings")
	for cport, bs := range bindings {
		for _, b := range bs {
			sp.Ports = append(sp.Ports, PortSpec{HostIP: b.HostIp, HostPort: b.HostPort, Container: strings.TrimSuffix(cport, "/tcp")})
		}
	}
	sort.Slice(sp.Ports, func(i, j int) bool { return sp.Ports[i].Container < sp.Ports[j].Container })
	for _, m := range in.Mounts {
		ms := MountSpec{Type: m.Type, Target: m.Destination, ReadOnly: !m.RW}
		switch m.Type {
		case "volume":
			ms.Source = m.Name
		default:
			ms.Source = m.Source
		}
		sp.Mounts = append(sp.Mounts, ms)
	}
	mode := get[string](in.HostConfig, "NetworkMode")
	for name, n := range in.NetworkSettings.Networks {
		ns := NetSpec{Name: name}
		for _, a := range n.Aliases {
			if !strings.HasPrefix(in.ID, a) {
				ns.Aliases = append(ns.Aliases, a)
			}
		}
		if n.IPAMConfig != nil {
			ns.IPv4 = n.IPAMConfig.IPv4Address
		}
		sp.Networks = append(sp.Networks, ns)
	}
	sort.Slice(sp.Networks, func(i, j int) bool { return sp.Networks[i].Name < sp.Networks[j].Name })
	if strings.HasPrefix(mode, "container:") || mode == "host" || mode == "none" {
		sp.NetworkMode = mode
		sp.Networks = nil
	}
	return sp
}

// createBody merges a Spec over a base create request (Config fields at the
// top level plus HostConfig), as expected by POST /containers/create.
func createBody(sp Spec, baseConfig, baseHost map[string]json.RawMessage) (map[string]any, error) {
	if strings.TrimSpace(sp.Image) == "" {
		return nil, badRequest("the image is required")
	}
	body := map[string]any{}
	for k, v := range baseConfig {
		body[k] = v
	}
	host := map[string]any{}
	for k, v := range baseHost {
		host[k] = v
	}
	body["Image"] = sp.Image
	body["Cmd"] = nilIfEmpty(sp.Cmd)
	body["Entrypoint"] = nilIfEmpty(sp.Entrypoint)
	body["Env"] = nilIfEmpty(sp.Env)
	body["Labels"] = sp.Labels
	body["Hostname"] = sp.Hostname
	body["User"] = sp.User
	body["WorkingDir"] = sp.WorkingDir
	body["Tty"] = sp.Tty
	body["OpenStdin"] = sp.OpenStdin
	body["AttachStdin"] = false
	delete(body, "Domainname")

	exposed := map[string]struct{}{}
	if raw, ok := baseConfig["ExposedPorts"]; ok {
		_ = json.Unmarshal(raw, &exposed)
	}
	bindings := map[string][]map[string]string{}
	for _, p := range sp.Ports {
		cp := strings.TrimSpace(p.Container)
		if cp == "" {
			continue
		}
		if !strings.Contains(cp, "/") {
			cp += "/tcp"
		}
		if _, err := strconv.Atoi(strings.Split(cp, "/")[0]); err != nil {
			return nil, badRequest("invalid container port: %s", p.Container)
		}
		exposed[cp] = struct{}{}
		bindings[cp] = append(bindings[cp], map[string]string{"HostIp": p.HostIP, "HostPort": p.HostPort})
	}
	body["ExposedPorts"] = exposed
	host["PortBindings"] = bindings

	var mounts []map[string]any
	for _, m := range sp.Mounts {
		if m.Target == "" {
			continue
		}
		t := m.Type
		if t == "" {
			t = "volume"
			if strings.HasPrefix(m.Source, "/") {
				t = "bind"
			}
		}
		mm := map[string]any{"Type": t, "Target": m.Target, "ReadOnly": m.ReadOnly}
		if t != "tmpfs" {
			mm["Source"] = m.Source
		}
		mounts = append(mounts, mm)
	}
	host["Mounts"] = mounts
	host["Binds"] = nil
	host["VolumesFrom"] = nil

	rp := map[string]any{"Name": "no"}
	if sp.Restart != "" {
		name, max, _ := strings.Cut(sp.Restart, ":")
		rp["Name"] = name
		if n, err := strconv.Atoi(max); err == nil {
			rp["MaximumRetryCount"] = n
		}
	}
	host["RestartPolicy"] = rp
	host["Privileged"] = sp.Privileged
	host["Memory"] = sp.Memory
	host["NanoCpus"] = sp.NanoCPUs
	host["CapAdd"] = nilIfEmpty(sp.CapAdd)
	host["ExtraHosts"] = nilIfEmpty(sp.ExtraHosts)

	endpoints := map[string]any{}
	switch {
	case sp.NetworkMode != "":
		host["NetworkMode"] = sp.NetworkMode
	case len(sp.Networks) > 0:
		host["NetworkMode"] = sp.Networks[0].Name
		for _, n := range sp.Networks {
			ep := map[string]any{"Aliases": nilIfEmpty(n.Aliases)}
			if n.IPv4 != "" {
				ep["IPAMConfig"] = map[string]string{"IPv4Address": n.IPv4}
			}
			endpoints[n.Name] = ep
		}
	default:
		host["NetworkMode"] = "bridge"
	}
	if host["NetworkMode"] != nil {
		if mode, _ := host["NetworkMode"].(string); strings.HasPrefix(mode, "container:") || mode == "host" {
			body["Hostname"] = ""
			delete(body, "ExposedPorts")
			host["PortBindings"] = nil
		}
	}
	body["HostConfig"] = host
	body["NetworkingConfig"] = map[string]any{"EndpointsConfig": endpoints}
	return body, nil
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}
