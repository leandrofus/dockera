package server

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"dockera/internal/agent"
	"dockera/internal/auth"
	"dockera/internal/docker"
	"dockera/internal/probe"
)

// The jump screen: every port of every container, classified as HTTP (one
// click away) or TCP/UDP (with a connection string when the service is known
// and its credentials can be read from the environment compose gave it).
//
// Compose labels can override the guesswork:
//   dockera.url=https://app.example.com    URL for the lowest HTTP port
//   dockera.url.<port>=...                 URL for a given container port
//   dockera.kind.<port>=http|tcp           force the tab of a port

type jumpEntry struct {
	ContainerID string   `json:"container_id"`
	Container   string   `json:"container"`
	Stack       string   `json:"stack,omitempty"`
	Service     string   `json:"service,omitempty"`
	Image       string   `json:"image"`
	State       string   `json:"state"`
	Port        int      `json:"port"`
	PublicPort  int      `json:"public_port,omitempty"`
	HostIP      string   `json:"host_ip,omitempty"`
	Proto       string   `json:"proto"`
	Kind        string   `json:"kind"`             // http | tcp
	Scheme      string   `json:"scheme,omitempty"` // http | https, for kind=http
	URL         string   `json:"url,omitempty"`    // from a dockera.url label
	App         string   `json:"app,omitempty"`    // detected service, e.g. PostgreSQL
	Conn        string   `json:"conn,omitempty"`   // connection string; {host} and {port} are filled by the UI
	Missing     []string `json:"missing,omitempty"`
	Masked      bool     `json:"masked,omitempty"`
}

type jumpResponse struct {
	Host    string      `json:"host"`   // host to reach published ports; empty = the browser's
	Via     string      `json:"via"`    // local | direct | agent: how the server reaches the host
	Tunnel  bool        `json:"tunnel"` // ports can be forwarded through this server
	Entries []jumpEntry `json:"entries"`
}

type listedContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
	Ports  []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

func (s *Server) jump(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var list []listedContainer
	if err := c.Get(r.Context(), "/containers/json", nil, &list); err != nil {
		writeErr(w, err)
		return
	}
	admin := sessionFrom(r.Context()).Role == auth.RoleAdmin
	envs := s.containerEnvs(r.Context(), c, list)

	tgt := s.jumpTarget(r.PathValue("ep"))
	out := jumpResponse{Host: tgt.host, Via: tgt.via, Tunnel: tgt.dial != nil, Entries: []jumpEntry{}}
	for _, ct := range list {
		seen := map[string]bool{}
		for _, p := range ct.Ports {
			// docker lists a port once per address family; keep one
			k := strconv.Itoa(p.PrivatePort) + "/" + p.Type + ":" + strconv.Itoa(p.PublicPort)
			if seen[k] {
				continue
			}
			seen[k] = true
			e := jumpEntry{
				ContainerID: ct.ID,
				Container:   strings.TrimPrefix(first(ct.Names), "/"),
				Stack:       ct.Labels["com.docker.compose.project"],
				Service:     ct.Labels["com.docker.compose.service"],
				Image:       ct.Image,
				State:       ct.State,
				Port:        p.PrivatePort,
				PublicPort:  p.PublicPort,
				HostIP:      p.IP,
				Proto:       p.Type,
			}
			classify(&e, ct.Labels, envs[ct.ID], admin)
			out.Entries = append(out.Entries, e)
		}
		if u := ct.Labels["dockera.url"]; u != "" {
			best := -1
			for i := len(out.Entries) - 1; i >= 0 && out.Entries[i].ContainerID == ct.ID; i-- {
				if x := out.Entries[i]; x.Kind == "http" && x.URL == "" && (best < 0 || x.Port < out.Entries[best].Port) {
					best = i
				}
			}
			if best >= 0 {
				out.Entries[best].URL = u
			}
		}
	}
	if tgt.dial != nil {
		probeUnknown(r.Context(), r.PathValue("ep"), out.Entries, tgt)
	}
	sort.SliceStable(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.Stack != b.Stack {
			return a.Stack < b.Stack
		}
		if a.Container != b.Container {
			return a.Container < b.Container
		}
		return a.Port < b.Port
	})
	writeJSON(w, out)
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// containerEnvs reads Config.Env of every container that has ports, which
// is the environment compose resolved (.env, interpolation) when it deployed.
func (s *Server) containerEnvs(ctx context.Context, c *docker.Client, list []listedContainer) map[string]map[string]string {
	out := map[string]map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, ct := range list {
		if len(ct.Ports) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			in, err := s.inspectRaw(ctx, c, ct.ID)
			if err != nil {
				return
			}
			env := map[string]string{}
			for _, kv := range get[[]string](in.Config, "Env") {
				k, v, _ := strings.Cut(kv, "=")
				env[k] = v
			}
			mu.Lock()
			out[ct.ID] = env
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// target says how to reach the published ports of an endpoint.
type target struct {
	host string // shown in links; empty = the browser's host
	// dial connects to a port as the endpoint's host sees it; nil when this
	// server cannot get there (ssh endpoints behind a firewall still try).
	dial func(ctx context.Context, addr string) (net.Conn, error)
	// local: dial addresses are the endpoint host's own (127.0.0.1 works)
	local bool
	via   string
}

func (s *Server) jumpTarget(name string) target {
	var d net.Dialer
	for _, ep := range s.Cfg.Snapshot().Endpoints {
		if ep.Name != name {
			continue
		}
		u, err := url.Parse(ep.URL)
		if err != nil {
			return target{}
		}
		switch u.Scheme {
		case "unix":
			return target{dial: func(ctx context.Context, a string) (net.Conn, error) { return d.DialContext(ctx, "tcp", a) }, local: true, via: "local"}
		case "ssh", "tcp":
			return target{host: u.Hostname(), via: "direct", dial: func(ctx context.Context, a string) (net.Conn, error) { return d.DialContext(ctx, "tcp", a) }}
		case "agent":
			agentName := strings.TrimPrefix(u.Host+u.Path, "/")
			if agentName == "" {
				agentName = ep.Name
			}
			host := agent.DefaultHub.Addr(agentName)
			if ip, err := netip.ParseAddr(host); err != nil || ip.IsLoopback() {
				host = ""
			}
			return target{host: host, local: true, via: "agent", dial: func(ctx context.Context, a string) (net.Conn, error) {
				return agent.DefaultHub.DialTarget(ctx, agentName, a)
			}}
		}
	}
	return target{}
}

// Probe results are cached: the page polls and ports rarely change protocol.
var probeCache = struct {
	sync.Mutex
	m map[string]probeResult
}{m: map[string]probeResult{}}

type probeResult struct {
	scheme string // http, https or "" (not HTTP)
	at     time.Time
}

// probeUnknown sniffs published TCP ports nobody recognized: whatever answers
// an HTTP request moves to the HTTP tab.
func probeUnknown(ctx context.Context, epName string, entries []jumpEntry, t target) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for i := range entries {
		e := &entries[i]
		if e.Kind != "tcp" || e.App != "" || e.Proto != "tcp" || e.PublicPort == 0 {
			continue
		}
		h := t.host
		if t.local {
			h = "127.0.0.1"
		}
		if e.HostIP != "" && e.HostIP != "0.0.0.0" && e.HostIP != "::" {
			h = e.HostIP
		}
		if h == "" {
			continue
		}
		addr := net.JoinHostPort(h, strconv.Itoa(e.PublicPort))
		key := epName + "|" + e.ContainerID + "|" + addr
		probeCache.Lock()
		res, ok := probeCache.m[key]
		probeCache.Unlock()
		if ok && time.Since(res.at) < 5*time.Minute {
			setScheme(e, res.scheme)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			scheme := probe.Sniff(ctx, func(ctx context.Context) (net.Conn, error) { return t.dial(ctx, addr) })
			probeCache.Lock()
			for k, v := range probeCache.m {
				if time.Since(v.at) > 10*time.Minute {
					delete(probeCache.m, k)
				}
			}
			probeCache.m[key] = probeResult{scheme, time.Now()}
			probeCache.Unlock()
			setScheme(e, scheme)
		}()
	}
	wg.Wait()
}

func setScheme(e *jumpEntry, scheme string) {
	if scheme != "" {
		e.Kind, e.Scheme = "http", scheme
	}
}

// app describes a well-known service: how to recognize it and how to build
// its connection string from the container environment.
type app struct {
	name   string
	images []string
	ports  []int
	http   []int // ports of this app that speak HTTP
	conn   func(env map[string]string) (tmpl string, user, pass string, missing []string)
}

func pick(env map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := env[k]; v != "" {
			return v
		}
	}
	return ""
}

// creds renders scheme://user:pass@{host}:{port}/path, noting what is unknown.
func creds(scheme, user, pass, path string, needPass bool) (string, string, string, []string) {
	var missing []string
	u := url.PathEscape(user)
	if user == "" && needPass {
		missing = append(missing, "user")
		u = "<user>"
	}
	if pass == "" && needPass {
		missing = append(missing, "password")
	}
	auth := ""
	switch {
	case u != "" && (pass != "" || needPass):
		auth = u + ":{pass}@"
	case pass != "":
		auth = ":{pass}@"
	case u != "":
		auth = u + "@"
	}
	return scheme + "://" + auth + "{host}:{port}" + path, user, pass, missing
}

var apps = []app{
	{name: "PostgreSQL", images: []string{"postgres", "postgis", "timescale", "pgvector", "supabase/postgres"}, ports: []int{5432},
		conn: func(env map[string]string) (string, string, string, []string) {
			user := pick(env, "POSTGRES_USER")
			if user == "" {
				user = "postgres"
			}
			db := pick(env, "POSTGRES_DB")
			if db == "" {
				db = user
			}
			return creds("postgresql", user, pick(env, "POSTGRES_PASSWORD"), "/"+url.PathEscape(db), env["POSTGRES_HOST_AUTH_METHOD"] != "trust")
		}},
	{name: "MariaDB", images: []string{"mariadb"}, ports: []int{3306}, conn: mysqlConn},
	{name: "MySQL", images: []string{"mysql", "percona"}, ports: []int{3306}, conn: mysqlConn},
	{name: "MongoDB", images: []string{"mongo"}, ports: []int{27017},
		conn: func(env map[string]string) (string, string, string, []string) {
			user, pass := pick(env, "MONGO_INITDB_ROOT_USERNAME", "MONGODB_ROOT_USER", "MONGODB_USERNAME"), pick(env, "MONGO_INITDB_ROOT_PASSWORD", "MONGODB_ROOT_PASSWORD", "MONGODB_PASSWORD")
			return creds("mongodb", user, pass, "/", false)
		}},
	{name: "Redis", images: []string{"redis", "valkey", "keydb", "dragonfly"}, ports: []int{6379},
		conn: func(env map[string]string) (string, string, string, []string) {
			return creds("redis", pick(env, "REDIS_USERNAME"), pick(env, "REDIS_PASSWORD", "VALKEY_PASSWORD"), "", false)
		}},
	{name: "RabbitMQ", images: []string{"rabbitmq"}, ports: []int{5672, 15672}, http: []int{15672},
		conn: func(env map[string]string) (string, string, string, []string) {
			user, pass := pick(env, "RABBITMQ_DEFAULT_USER"), pick(env, "RABBITMQ_DEFAULT_PASS")
			if user == "" && pass == "" {
				user, pass = "guest", "guest"
			}
			vhost := pick(env, "RABBITMQ_DEFAULT_VHOST")
			if vhost == "/" {
				vhost = ""
			}
			return creds("amqp", user, pass, "/"+url.PathEscape(vhost), true)
		}},
	{name: "SQL Server", images: []string{"mssql", "sqlserver", "azure-sql-edge"}, ports: []int{1433},
		conn: func(env map[string]string) (string, string, string, []string) {
			return creds("sqlserver", "sa", pick(env, "MSSQL_SA_PASSWORD", "SA_PASSWORD"), "", true)
		}},
	{name: "ClickHouse", images: []string{"clickhouse"}, ports: []int{9000, 8123}, http: []int{8123},
		conn: func(env map[string]string) (string, string, string, []string) {
			user := pick(env, "CLICKHOUSE_USER")
			if user == "" {
				user = "default"
			}
			return creds("clickhouse", user, pick(env, "CLICKHOUSE_PASSWORD"), "/"+url.PathEscape(pick(env, "CLICKHOUSE_DB")), false)
		}},
	{name: "MinIO", images: []string{"minio"}, ports: []int{9000, 9001}, http: []int{9000, 9001}},
	{name: "Elasticsearch", images: []string{"elasticsearch", "opensearch"}, ports: []int{9200}, http: []int{9200}},
	{name: "Memcached", images: []string{"memcached"}, ports: []int{11211}, conn: plain("memcached")},
	{name: "NATS", images: []string{"nats"}, ports: []int{4222, 8222}, http: []int{8222}, conn: plain("nats")},
	{name: "Kafka", images: []string{"kafka", "redpanda"}, ports: []int{9092}, conn: func(map[string]string) (string, string, string, []string) { return "{host}:{port}", "", "", nil }},
	{name: "Mailpit", images: []string{"mailpit", "mailhog"}, ports: []int{1025, 8025}, http: []int{8025}, conn: plain("smtp")},
	{name: "SSH", images: []string{"openssh", "sshd"}, ports: []int{22, 2222},
		conn: func(env map[string]string) (string, string, string, []string) {
			user := pick(env, "USER_NAME", "SSH_USER")
			if user == "" {
				return "ssh -p {port} <user>@{host}", "", "", []string{"user"}
			}
			return "ssh -p {port} " + user + "@{host}", "", "", nil
		}},
}

func plain(scheme string) func(map[string]string) (string, string, string, []string) {
	return func(map[string]string) (string, string, string, []string) {
		return scheme + "://{host}:{port}", "", "", nil
	}
}

func mysqlConn(env map[string]string) (string, string, string, []string) {
	user, pass := pick(env, "MYSQL_USER", "MARIADB_USER"), pick(env, "MYSQL_PASSWORD", "MARIADB_PASSWORD")
	if user == "" {
		user, pass = "root", pick(env, "MYSQL_ROOT_PASSWORD", "MARIADB_ROOT_PASSWORD")
		if pass == "" && pick(env, "MYSQL_ALLOW_EMPTY_PASSWORD", "MARIADB_ALLOW_EMPTY_ROOT_PASSWORD") != "" {
			return "mysql://root@{host}:{port}/" + url.PathEscape(pick(env, "MYSQL_DATABASE", "MARIADB_DATABASE")), "", "", nil
		}
	}
	return creds("mysql", user, pass, "/"+url.PathEscape(pick(env, "MYSQL_DATABASE", "MARIADB_DATABASE")), true)
}

// Ports that almost always speak HTTP, and images whose every port does.
var (
	httpPorts  = map[int]bool{80: true, 443: true, 3000: true, 3001: true, 4000: true, 5000: true, 5173: true, 5601: true, 8000: true, 8080: true, 8081: true, 8082: true, 8088: true, 8443: true, 8888: true, 9090: true, 9443: true}
	httpsPorts = map[int]bool{443: true, 8443: true, 9443: true}
	httpImages = []string{"nginx", "httpd", "traefik", "caddy", "grafana", "prometheus", "portainer", "jenkins", "gitea", "nextcloud", "wordpress", "adminer", "phpmyadmin", "pgadmin", "uptime-kuma", "jellyfin", "homeassistant", "home-assistant", "n8n", "kibana", "code-server", "vaultwarden", "keycloak", "sonarqube"}
)

// imageName strips registry, tag and digest: "ghcr.io/x/postgres:16" -> "x/postgres".
func imageName(ref string) string {
	ref, _, _ = strings.Cut(ref, "@")
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	if parts := strings.SplitN(ref, "/", 2); len(parts) == 2 && strings.ContainsAny(parts[0], ".:") {
		ref = parts[1]
	}
	return strings.ToLower(ref)
}

func matchImage(name string, list []string) bool {
	last := name[strings.LastIndex(name, "/")+1:]
	for _, m := range list {
		if strings.Contains(m, "/") {
			if strings.Contains(name, m) {
				return true
			}
		} else if strings.HasPrefix(last, m) {
			return true
		}
	}
	return false
}

func contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func classify(e *jumpEntry, labels map[string]string, env map[string]string, admin bool) {
	e.Kind = "tcp"
	img := imageName(e.Image)
	var found *app
	for i := range apps {
		a := &apps[i]
		if matchImage(img, a.images) && (contains(a.ports, e.Port) || len(a.ports) == 0) {
			found = a
			break
		}
	}
	if found == nil && !httpPorts[e.Port] {
		// no image match: fall back to the port alone
		for i := range apps {
			if apps[i].conn != nil && len(apps[i].ports) > 0 && apps[i].ports[0] == e.Port && e.Port != 9000 {
				found = &apps[i]
				break
			}
		}
	}
	if found != nil {
		e.App = found.name
	}
	switch {
	case e.Proto != "tcp":
	case found != nil && contains(found.http, e.Port):
		e.Kind = "http"
	case found != nil:
	case httpPorts[e.Port] || matchImage(img, httpImages):
		e.Kind = "http"
	}
	if k := labels["dockera.kind."+strconv.Itoa(e.Port)]; k == "http" || k == "tcp" {
		e.Kind = k
	}
	if e.Kind == "http" {
		e.Scheme = "http"
		if httpsPorts[e.Port] {
			e.Scheme = "https"
		}
		e.URL = labels["dockera.url."+strconv.Itoa(e.Port)]
		return
	}
	if found == nil || found.conn == nil || e.Proto != "tcp" {
		return
	}
	tmpl, _, pass, missing := found.conn(env)
	e.Missing = missing
	if strings.Contains(tmpl, "{pass}") {
		if pass == "" {
			tmpl = strings.ReplaceAll(tmpl, "{pass}", "<password>")
		} else if admin {
			tmpl = strings.ReplaceAll(tmpl, "{pass}", url.PathEscape(pass))
		} else {
			tmpl = strings.ReplaceAll(tmpl, "{pass}", "••••")
			e.Masked = true
		}
	}
	e.Conn = tmpl
}
