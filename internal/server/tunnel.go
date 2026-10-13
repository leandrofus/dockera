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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ducktainer/internal/access"
	"ducktainer/internal/auth"
)

// Ephemeral port forwards: the server listens on a port from TunnelPorts and
// pipes every connection to a container port of an endpoint (through the
// agent tunnel for nodes behind NAT). A forward only accepts the address
// that asked for it and closes after TunnelIdle without connections.

const (
	defaultTunnelPorts = "41000-41099"
	TunnelIdle         = 30 * time.Minute
)

type tunnel struct {
	ID          string    `json:"id"`
	Endpoint    string    `json:"endpoint"`
	Container   string    `json:"container"`
	ContainerID string    `json:"container_id"`
	Port        int       `json:"port"`   // container port
	Proto       string    `json:"proto"`  // always tcp
	Target      string    `json:"target"` // address dialed on the endpoint host
	Local       int       `json:"local_port"`
	Client      string    `json:"client"`
	User        string    `json:"user"`
	Created     time.Time `json:"created"`
	Expires     time.Time `json:"expires"`
	Active      int       `json:"active"`

	ln   net.Listener
	dial func(ctx context.Context, addr string) (net.Conn, error)
}

type tunnels struct {
	mu   sync.Mutex
	byID map[string]*tunnel
	once sync.Once
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

func (s *Server) openTunnel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ContainerID string `json:"container_id"`
		Port        int    `json:"port"`
		PublicPort  int    `json:"public_port"`
		HostIP      string `json:"host_ip"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	epName := r.PathValue("ep")
	tgt := s.jumpTarget(epName)
	if tgt.dial == nil {
		writeErr(w, badRequest("this host cannot be tunneled"))
		return
	}
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
	// Resolve the address on the endpoint host: the published port when there
	// is one, the container's own IP otherwise (internal ports).
	var addr string
	if body.PublicPort > 0 {
		h := "127.0.0.1"
		if !tgt.local {
			h = tgt.host
		}
		if ip, err := netip.ParseAddr(body.HostIP); err == nil && !ip.IsUnspecified() {
			h = ip.String()
		}
		addr = net.JoinHostPort(h, strconv.Itoa(body.PublicPort))
	} else {
		if !tgt.local {
			writeErr(w, badRequest("internal ports can only be tunneled on local and agent hosts"))
			return
		}
		ip := containerIP(in)
		if ip == "" {
			writeErr(w, badRequest("the container has no IP address (is it running?)"))
			return
		}
		addr = net.JoinHostPort(ip, strconv.Itoa(body.Port))
	}
	client := access.RemoteIP(r, false).String()
	sess := sessionFrom(r.Context())

	tm := &s.tunnels
	tm.once.Do(func() {
		tm.byID = map[string]*tunnel{}
		go s.reapTunnels()
	})
	tm.mu.Lock()
	for _, t := range tm.byID {
		if t.Endpoint == epName && t.Target == addr && t.Client == client {
			t.Expires = time.Now().Add(TunnelIdle)
			tm.mu.Unlock()
			writeJSON(w, t)
			return
		}
	}
	tm.mu.Unlock()

	lo, hi, err := s.tunnelRange()
	if err != nil {
		writeErr(w, err)
		return
	}
	// make sure the target answers before taking a port
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	probeConn, err := tgt.dial(ctx, addr)
	cancel()
	if err != nil {
		writeErr(w, herr(http.StatusBadGateway, "cannot reach %s: %v", addr, err))
		return
	}
	probeConn.Close()

	var ln net.Listener
	for p := lo; p <= hi && ln == nil; p++ {
		ln, _ = net.Listen("tcp", net.JoinHostPort(s.tunnelBind(), strconv.Itoa(p)))
	}
	if ln == nil {
		writeErr(w, herr(http.StatusServiceUnavailable, "no free port left in the tunnel range %d-%d", lo, hi))
		return
	}
	id := make([]byte, 6)
	rand.Read(id)
	t := &tunnel{
		ID: hex.EncodeToString(id), Endpoint: epName, ContainerID: body.ContainerID,
		Container: strings.TrimPrefix(in.Name, "/"), Port: body.Port, Proto: "tcp", Target: addr,
		Local: ln.Addr().(*net.TCPAddr).Port, Client: client, User: sess.User,
		Created: time.Now(), Expires: time.Now().Add(TunnelIdle), ln: ln, dial: tgt.dial,
	}
	tm.mu.Lock()
	tm.byID[t.ID] = t
	tm.mu.Unlock()
	log.Printf("tunnel %s: %s:%d -> %s %s (for %s)", t.ID, s.tunnelBind(), t.Local, epName, addr, client)
	go s.serveTunnel(t)
	writeJSON(w, t)
}

func containerIP(in *inspect) string {
	names := make([]string, 0, len(in.NetworkSettings.Networks))
	for n := range in.NetworkSettings.Networks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if ip := in.NetworkSettings.Networks[n].IPAddress; ip != "" {
			return ip
		}
	}
	return ""
}

func (s *Server) serveTunnel(t *tunnel) {
	for {
		conn, err := t.ln.Accept()
		if err != nil {
			return
		}
		ip, _ := netip.ParseAddrPort(conn.RemoteAddr().String())
		if ip.Addr().Unmap().String() != t.Client {
			conn.Close()
			continue
		}
		go s.pipeTunnel(t, conn)
	}
}

func (s *Server) pipeTunnel(t *tunnel, conn net.Conn) {
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	up, err := t.dial(ctx, t.Target)
	cancel()
	if err != nil {
		log.Printf("tunnel %s: %v", t.ID, err)
		return
	}
	defer up.Close()
	s.tunnels.mu.Lock()
	t.Active++
	s.tunnels.mu.Unlock()
	defer func() {
		s.tunnels.mu.Lock()
		t.Active--
		t.Expires = time.Now().Add(TunnelIdle)
		s.tunnels.mu.Unlock()
	}()
	done := make(chan struct{}, 2)
	go func() { io.Copy(up, conn); done <- struct{}{} }()
	go func() { io.Copy(conn, up); done <- struct{}{} }()
	<-done
}

func (s *Server) reapTunnels() {
	for range time.Tick(time.Minute) {
		s.tunnels.mu.Lock()
		for id, t := range s.tunnels.byID {
			if t.Active == 0 && time.Now().After(t.Expires) {
				t.ln.Close()
				delete(s.tunnels.byID, id)
				log.Printf("tunnel %s closed (idle)", id)
			}
		}
		s.tunnels.mu.Unlock()
	}
}

// listTunnels returns the forwards of the caller's address (admins: all).
func (s *Server) listTunnels(w http.ResponseWriter, r *http.Request) {
	client := access.RemoteIP(r, false).String()
	out := []tunnel{}
	s.tunnels.mu.Lock()
	for _, t := range s.tunnels.byID {
		if t.Client == client || (r.URL.Query().Get("all") == "1" && sessionFrom(r.Context()).Role == auth.RoleAdmin) {
			out = append(out, *t)
		}
	}
	s.tunnels.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	writeJSON(w, out)
}

func (s *Server) closeTunnel(w http.ResponseWriter, r *http.Request) {
	s.tunnels.mu.Lock()
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
	writeJSON(w, map[string]bool{"ok": true})
}
