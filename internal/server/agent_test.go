package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dockera/internal/access"
	"dockera/internal/agent"
	"dockera/internal/config"
	"dockera/internal/server"
)

func newTestServer(t *testing.T) (*server.Server, *config.Config, func()) {
	tmpDir, err := os.MkdirTemp("", "dockera-srv-test-*")
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("DOCKERA_HOME", tmpDir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Token = "test-admin-token"
	_ = cfg.Save()

	acl, err := access.New([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}

	srv := server.New(cfg, acl, os.DirFS(tmpDir))
	cleanup := func() {
		os.RemoveAll(tmpDir)
		os.Unsetenv("DOCKERA_HOME")
	}
	return srv, cfg, cleanup
}

func TestAgentInstallScriptAndBinary(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()

	handler := srv.Handler()

	// 1. Test /api/agent/binary without token -> 401
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/agent/binary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for /api/agent/binary without token, got %d", rec.Code)
	}

	// 2. Test /api/agent/binary with valid token -> 200
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/agent/binary?token=test-admin-token", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/agent/binary with valid token, got %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "dockera-agent") {
		t.Fatalf("expected Content-Disposition header, got %q", cd)
	}

	// 3. Test /api/agent/install.sh with valid token -> 200 shell script
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/agent/install.sh?token=test-admin-token&name=edge-node", nil)
	req.Host = "dockera.central:9900"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/agent/install.sh, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "#!/bin/sh") {
		t.Fatalf("expected #!/bin/sh in install script, got:\n%s", body)
	}
	if !strings.Contains(body, "NAME=\"edge-node\"") || !strings.Contains(body, "TOKEN=\"test-admin-token\"") {
		t.Fatalf("expected NAME and TOKEN in install script, got:\n%s", body)
	}
}

func TestAgentAutoRegistrationAndDial(t *testing.T) {
	srv, cfg, cleanup := newTestServer(t)
	defer cleanup()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Create mock docker socket
	tmpDir, _ := os.MkdirTemp("", "dockera-agent-mock-*")
	defer os.RemoveAll(tmpDir)
	mockSock := filepath.Join(tmpDir, "docker.sock")
	ln, err := os.Create(mockSock)
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()

	// Connect agent using admin token
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentCfg := agent.Config{
		Server:   ts.URL,
		Token:    "test-admin-token",
		Name:     "auto-worker-1",
		Socket:   mockSock,
		Insecure: true,
	}

	go func() {
		_ = agent.Run(ctx, agentCfg)
	}()

	// Wait for agent to connect
	connected := false
	for i := 0; i < 40; i++ {
		if agent.DefaultHub.IsOnline("auto-worker-1") {
			connected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !connected {
		t.Fatalf("agent failed to auto-register and connect to server")
	}

	// Verify that the endpoint was automatically registered in config
	snap := cfg.Snapshot()
	found := false
	for _, ep := range snap.Endpoints {
		if ep.Name == "auto-worker-1" && ep.URL == "agent://auto-worker-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected auto-worker-1 to be saved in cfg.Endpoints, got: %+v", snap.Endpoints)
	}
}
