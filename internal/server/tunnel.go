package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ducktainer/internal/access"
	"ducktainer/internal/auth"
	"ducktainer/internal/config"
)

// Port forwards ("private tunnels"): the server listens on a port from
// TunnelPorts and pipes every connection to a container port of an
// endpoint (through the agent tunnel for nodes behind NAT). They last:
//   - idle: until TunnelIdle passes without connections (in memory only)
//   - until: up to a fixed time
//   - persistent: until someone closes them
// The last two are saved and come back, on the same port when it is free,
// after a restart. A forward accepts only the address that opened it, or
// (shared) every address allowed to reach ducktainer.

const (
	defaultTunnelPorts = "41000-41099"
	TunnelIdle         = 30 * time.Minute
)

type tunnel struct {
	ID          string    `json:"id"`
	Endpoint    string    `json:"endpoint"`
	Container   string    `json:"container"`
	ContainerID string    `json:"container_id,omitempty"`
	Port        int       `json:"port"`  // container port
	Proto       string    `json:"proto"` // always tcp
	Local       int       `json:"local_port"`
	Mode        string    `json:"mode"`             // idle | until | persistent
	Expires     time.Time `json:"expires,omitzero"` // idle: rolling; until: fixed
	Shared      bool      `json:"shared"`
	Client      string    `json:"client"`
	User        string    `json:"user"`
	Created     time.Time `json:"created"`
	LastUsed    time.Time `json:"last_used,omitzero"`
	Active      int       `json:"active"`
	Mine        bool      `json:"mine"` // opened from the caller's address

	ln     net.Listener
	target *targetCache // shared by the copies handed out
}

type tunnels struct {
	mu   sync.Mutex
	byID map[string]*tunnel
	once sync.Once
}

// targetCache remembers where a container port answers for a while; a
// failed dial forgets it (the container may have been recreated).
type targetCache struct {
	mu   sync.Mutex
	addr string
	dial func(ctx context.Context, addr string) (net.Conn, error)
	at   time.Time
}

func (c *targetCache) connect(ctx context.Context, resolve func(context.Context) (string, func(context.Context, string) (net.Conn, error), error)) (net.Conn, error) {
	c.mu.Lock()
	addr, dial, fresh := c.addr, c.dial, time.Since(c.at) < targetMaxAge
	c.mu.Unlock()
	if !fresh || dial == nil {
		var err error
		if addr, dial, err = resolve(ctx); err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.addr, c.dial, c.at = addr, dial, time.Now()
		c.mu.Unlock()
	}
	conn, err := dial(ctx, addr)
	if err != nil {
		c.mu.Lock()
		c.at = time.Time{}
		c.mu.Unlock()
	}
	return conn, err
}

func (s *Server) tunnelRange() (int, int, error) {
	spec := s.TunnelPorts
	if spec == "" {
		spec = defaultTunnelPorts
	}
	a, b, ok := strings.Cut(spec, "-")
	lo, err1 := strconv.Atoi(strings.TrimSpace(a))
	hi, err2 := strconv.Atoi(strings.TrimSpace(b))
	if !ok || err1 != nil || err2 != nil || lo < 1 || hi > 65535 || lo > hi {
		return 0, 0, fmt.Errorf("invalid tunnel port range %q (want e.g. 41000-41099)", spec)
	}
	return lo, hi, nil
}

// tunnelBind is the address forwards listen on: the host of --listen.
func (s *Server) tunnelBind() string {
	host, _, err := net.SplitHostPort(s.Listen)
	if err != nil {
		return ""
	}
	return host
}

func (s *Server) tunnelMap() *tunnels {
	tm := &s.tunnels
	tm.once.Do(func() {
		tm.byID = map[string]*tunnel{}
		go s.reapTunnels()
	})
	return tm
}

// listenTunnel takes the preferred port when it is free, any port of the
// range otherwise.
func (s *Server) listenTunnel(prefer int) (net.Listener, error) {
	lo, hi, err := s.tunnelRange()
	if err != nil {
		return nil, err
	}
	if prefer >= lo && prefer <= hi {
		if ln, err := net.Listen("tcp", net.JoinHostPort(s.tunnelBind(), strconv.Itoa(prefer))); err == nil {
			return ln, nil
		}
	}
	for p := lo; p <= hi; p++ {
		if ln, err := net.Listen("tcp", net.JoinHostPort(s.tunnelBind(), strconv.Itoa(p))); err == nil {
			return ln, nil
		}
	}
	return nil, herr(http.StatusServiceUnavailable, "no free port left in the tunnel range %d-%d", lo, hi)
}

// tunnelMode turns the request into a mode and an expiry.
func tunnelMode(mode string, hours int) (string, time.Time, error) {
	switch mode {
	case "", "idle":
		return "idle", time.Now().Add(TunnelIdle), nil
	case "until":
		if hours < 1 || hours > 24*30 {
			return "", time.Time{}, badRequest("pick between 1 hour and 30 days")
		}
		return "until", time.Now().Add(time.Duration(hours) * time.Hour), nil
	case "persistent":
		return "persistent", time.Time{}, nil
	}
	return "", time.Time{}, badRequest("unknown duration: %s", mode)
}

func (t *tunnel) forward() config.Forward {
	f := config.Forward{ID: t.ID, Endpoint: t.Endpoint, Container: t.Container, Port: t.Port, Local: t.Local, Mode: t.Mode, Shared: t.Shared, Client: t.Client, User: t.User, Created: t.Created}
	if t.Mode == "until" {
		u := t.Expires
		f.Until = &u
	}
	return f
}

// saveForward keeps (or forgets, for idle ones) a forward in the config.
func (s *Server) saveForward(t tunnel) {
	err := s.Cfg.Update(func(c *config.Config) error {
		c.Forwards = slices.DeleteFunc(c.Forwards, func(f config.Forward) bool { return f.ID == t.ID })
		if t.Mode == "until" || t.Mode == "persistent" {
			c.Forwards = append(c.Forwards, t.forward())
		}
		return nil
	})
	if err != nil {
		log.Printf("tunnel %s: saving: %v", t.ID, err)
	}
}

func (s *Server) forgetForward(id string) {
	err := s.Cfg.Update(func(c *config.Config) error {
		c.Forwards = slices.DeleteFunc(c.Forwards, func(f config.Forward) bool { return f.ID == id })
		return nil
	})
	if err != nil {
		log.Printf("tunnel %s: %v", id, err)
	}
}

// StartTunnels brings back the saved forwards (serve calls it once).
func (s *Server) StartTunnels() {
	tm := s.tunnelMap()
	for _, f := range s.Cfg.Snapshot().Forwards {
		if f.Mode == "until" && (f.Until == nil || time.Now().After(*f.Until)) {
			s.forgetForward(f.ID)
			continue
		}
		ln, err := s.listenTunnel(f.Local)
		if err != nil {
			log.Printf("tunnel %s: %v", f.ID, err)
			continue
		}
		t := &tunnel{ID: f.ID, Endpoint: f.Endpoint, Container: f.Container, Port: f.Port, Proto: "tcp", Local: ln.Addr().(*net.TCPAddr).Port,
			Mode: f.Mode, Shared: f.Shared, Client: f.Client, User: f.User, Created: f.Created, ln: ln, target: &targetCache{}}
		if f.Until != nil {
			t.Expires = *f.Until
		}
		tm.mu.Lock()
		tm.byID[t.ID] = t
		tm.mu.Unlock()
		if t.Local != f.Local {
			log.Printf("tunnel %s: port %d is taken, now on %d", t.ID, f.Local, t.Local)
			s.saveForward(*t)
		}
		go s.serveTunnel(t)
	}
}

func (s *Server) openTunnel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ContainerID string `json:"container_id"`
		Container   string `json:"container"`
		Port        int    `json:"port"`
		Mode        string `json:"mode"` // idle (default) | until | persistent
		Hours       int    `json:"hours"`
		Shared      bool   `json:"shared"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	mode, expires, err := tunnelMode(body.Mode, body.Hours)
	if err != nil {
		writeErr(w, err)
		return
	}
	epName := r.PathValue("ep")
	name := strings.TrimPrefix(body.Container, "/")
	if name == "" {
		c, err := s.ep(r)
		if err != nil {
			writeErr(w, err)
			return
		}
		in, err := s.inspectRaw(r.Context(), c, body.ContainerID)
		if err != nil {
			writeErr(w, err)
			return
		}
		name = strings.TrimPrefix(in.Name, "/")
	}
	// make sure the target answers before taking a port
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	addr, dial, err := s.resolveTarget(ctx, epName, name, body.Port)
	if err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	probeConn, err := dial(ctx, addr)
	if err != nil {
		writeErr(w, herr(http.StatusBadGateway, "cannot reach %s: %v", addr, err))
		return
	}
	probeConn.Close()

	client := access.RemoteIP(r, false).String()
	user := sessionFrom(r.Context()).User
	tm := s.tunnelMap()
	tm.mu.Lock()
	for _, t := range tm.byID {
		if t.Endpoint == epName && t.Container == name && t.Port == body.Port && (t.Client == client || t.Shared) {
			// already open: a quick open refreshes it, a chosen duration replaces its own
			if body.Mode != "" {
				t.Mode, t.Expires, t.Shared = mode, expires, body.Shared
			} else if t.Mode == "idle" {
				t.Expires = expires
			}
			out := *t
			tm.mu.Unlock()
			s.saveForward(out)
			out.Mine = out.Client == client
			writeJSON(w, &out)
			return
		}
	}
	tm.mu.Unlock()

	ln, err := s.listenTunnel(0)
	if err != nil {
		writeErr(w, err)
		return
	}
	id := make([]byte, 6)
	rand.Read(id)
	t := &tunnel{
		ID: hex.EncodeToString(id), Endpoint: epName, ContainerID: body.ContainerID, Container: name, Port: body.Port, Proto: "tcp",
		Local: ln.Addr().(*net.TCPAddr).Port, Mode: mode, Expires: expires, Shared: body.Shared, Client: client, User: user,
		Created: time.Now().UTC(), ln: ln, target: &targetCache{},
	}
	tm.mu.Lock()
	tm.byID[t.ID] = t
	tm.mu.Unlock()
	s.saveForward(*t)
	log.Printf("tunnel %s: %s:%d -> %s %s:%d (%s, for %s)", t.ID, s.tunnelBind(), t.Local, epName, name, t.Port, mode, client)
	go s.serveTunnel(t)
	out := *t
	out.Mine = true
	writeJSON(w, &out)
}

func (s *Server) tunnelAllows(t *tunnel, a netip.Addr) bool {
	s.tunnels.mu.Lock()
	shared, client := t.Shared, t.Client
	s.tunnels.mu.Unlock()
	if shared {
		return s.Access.Allowed(a)
	}
	return a.Unmap().String() == client
}

func (s *Server) serveTunnel(t *tunnel) {
	for {
		conn, err := t.ln.Accept()
		if err != nil {
			return
		}
		ip, _ := netip.ParseAddrPort(conn.RemoteAddr().String())
		if !s.tunnelAllows(t, ip.Addr()) {
			conn.Close()
			continue
		}
		go s.pipeTunnel(t, conn)
	}
}

func (s *Server) pipeTunnel(t *tunnel, conn net.Conn) {
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	up, err := t.target.connect(ctx, func(ctx context.Context) (string, func(context.Context, string) (net.Conn, error), error) {
		return s.resolveTarget(ctx, t.Endpoint, t.Container, t.Port)
	})
	cancel()
	if err != nil {
		log.Printf("tunnel %s: %v", t.ID, err)
		return
	}
	defer up.Close()
	s.tunnels.mu.Lock()
	t.Active++
	t.LastUsed = time.Now().UTC()
	s.tunnels.mu.Unlock()
	defer func() {
		s.tunnels.mu.Lock()
		t.Active--
		t.LastUsed = time.Now().UTC()
		if t.Mode == "idle" {
			t.Expires = time.Now().Add(TunnelIdle)
		}
		s.tunnels.mu.Unlock()
	}()
	done := make(chan struct{}, 2)
	go func() { io.Copy(up, conn); done <- struct{}{} }()
	go func() { io.Copy(conn, up); done <- struct{}{} }()
	<-done
}

func (s *Server) reapTunnels() {
	for range time.Tick(time.Minute) {
		var expired []string
		s.tunnels.mu.Lock()
		for id, t := range s.tunnels.byID {
			due := !t.Expires.IsZero() && time.Now().After(t.Expires)
			if (t.Mode == "idle" && due && t.Active == 0) || (t.Mode == "until" && due) {
				t.ln.Close()
				delete(s.tunnels.byID, id)
				expired = append(expired, id)
				log.Printf("tunnel %s closed (%s)", id, t.Mode)
			}
		}
		s.tunnels.mu.Unlock()
		for _, id := range expired {
			s.forgetForward(id)
		}
	}
}

// listTunnels returns the forwards the caller can use (admins with ?all=1:
// every one).
func (s *Server) listTunnels(w http.ResponseWriter, r *http.Request) {
	client := access.RemoteIP(r, false).String()
	all := r.URL.Query().Get("all") == "1" && sessionFrom(r.Context()).Role == auth.RoleAdmin
	out := []tunnel{}
	s.tunnelMap().mu.Lock()
	for _, t := range s.tunnels.byID {
		if t.Client == client || t.Shared || all {
			c := *t
			c.Mine = t.Client == client
			out = append(out, c)
		}
	}
	s.tunnels.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	writeJSON(w, out)
}

// updateTunnel changes how long a forward lasts and who may use it.
func (s *Server) updateTunnel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode   string `json:"mode"`
		Hours  int    `json:"hours"`
		Shared bool   `json:"shared"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	mode, expires, err := tunnelMode(body.Mode, body.Hours)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.tunnelMap().mu.Lock()
	t, ok := s.tunnels.byID[r.PathValue("id")]
	var out tunnel
	if ok {
		t.Mode, t.Expires, t.Shared = mode, expires, body.Shared
		out = *t
	}
	s.tunnels.mu.Unlock()
	if !ok {
		writeErr(w, herr(http.StatusNotFound, "no such tunnel"))
		return
	}
	s.saveForward(out)
	out.Mine = out.Client == access.RemoteIP(r, false).String()
	writeJSON(w, &out)
}

func (s *Server) closeTunnel(w http.ResponseWriter, r *http.Request) {
	s.tunnelMap().mu.Lock()
	t, ok := s.tunnels.byID[r.PathValue("id")]
	if ok {
		t.ln.Close()
		delete(s.tunnels.byID, t.ID)
	}
	s.tunnels.mu.Unlock()
	if !ok {
		writeErr(w, herr(http.StatusNotFound, "no such tunnel"))
		return
	}
	s.forgetForward(t.ID)
	writeJSON(w, map[string]bool{"ok": true})
}
