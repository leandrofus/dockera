package server

import (
	"net"
	"net/netip"
	"os"
	"strconv"
	"testing"
	"time"

	"ducktainer/internal/access"
	"ducktainer/internal/config"
)

func TestTunnelMode(t *testing.T) {
	if m, exp, err := tunnelMode("", 0); err != nil || m != "idle" || time.Until(exp) < 29*time.Minute {
		t.Fatalf("default: %s %v %v", m, exp, err)
	}
	if m, exp, err := tunnelMode("until", 4); err != nil || m != "until" || time.Until(exp) < 239*time.Minute {
		t.Fatalf("hours: %s %v %v", m, exp, err)
	}
	if m, exp, err := tunnelMode("persistent", 0); err != nil || m != "persistent" || !exp.IsZero() {
		t.Fatalf("persistent: %s %v %v", m, exp, err)
	}
	for _, bad := range []struct {
		m string
		h int
	}{{"until", 0}, {"until", 24*30 + 1}, {"forever", 0}} {
		if _, _, err := tunnelMode(bad.m, bad.h); err == nil {
			t.Errorf("%s/%d accepted", bad.m, bad.h)
		}
	}
}

func TestStartTunnelsRestores(t *testing.T) {
	t.Setenv("DUCKTAINER_HOME", t.TempDir())
	t.Setenv("DUCKTAINER_KEY_FILE", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	// a free port inside the range
	probe, _ := net.Listen("tcp", "127.0.0.1:0")
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	cfg.Update(func(c *config.Config) error {
		c.Forwards = []config.Forward{
			{ID: "keep", Endpoint: "local", Container: "db", Port: 5432, Local: port, Mode: "persistent", Client: "10.0.0.1"},
			{ID: "later", Endpoint: "local", Container: "web", Port: 80, Mode: "until", Until: &future, Shared: true},
			{ID: "gone", Endpoint: "local", Container: "old", Port: 80, Mode: "until", Until: &past},
		}
		return nil
	})
	acl, _ := access.New([]string{"192.0.2.0/24"})
	s := New(cfg, acl, os.DirFS(t.TempDir()))
	s.Listen = "127.0.0.1:9900"
	s.TunnelPorts = strconv.Itoa(port) + "-" + strconv.Itoa(port+20)
	s.StartTunnels()
	t.Cleanup(func() {
		for _, tn := range s.tunnels.byID {
			tn.ln.Close()
		}
	})
	keep, later := s.tunnels.byID["keep"], s.tunnels.byID["later"]
	if keep == nil || later == nil || s.tunnels.byID["gone"] != nil {
		t.Fatalf("restored: %v", s.tunnels.byID)
	}
	if keep.Local != port {
		t.Errorf("persistent forward moved from %d to %d", port, keep.Local)
	}
	if n := len(s.Cfg.Snapshot().Forwards); n != 2 {
		t.Errorf("expired forward still saved (%d saved)", n)
	}
	// only its client, or (shared) whatever the allowlist allows
	if !s.tunnelAllows(keep, netip.MustParseAddr("10.0.0.1")) || s.tunnelAllows(keep, netip.MustParseAddr("10.0.0.2")) {
		t.Error("private forward: wrong client check")
	}
	if !s.tunnelAllows(later, netip.MustParseAddr("192.0.2.7")) || s.tunnelAllows(later, netip.MustParseAddr("203.0.113.1")) {
		t.Error("shared forward: wrong allowlist check")
	}
}
