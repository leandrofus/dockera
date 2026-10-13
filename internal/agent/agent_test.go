package agent_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"ducktainer/internal/agent"
	"ducktainer/internal/probe"
)

func TestHubBasics(t *testing.T) {
	hub := agent.NewHub()

	if hub.IsOnline("worker-1") {
		t.Fatal("expected worker-1 to be offline")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := hub.Dial(ctx, "worker-1")
	if err == nil {
		t.Fatal("expected error dialing offline worker-1")
	}
}

func TestAgentTunnelAndDial(t *testing.T) {
	// 1. Mock local Docker daemon listening on a temporary unix socket
	tmpDir, err := os.MkdirTemp("", "ducktainer-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	mockSock := filepath.Join(tmpDir, "docker.sock")
	sockLn, err := net.Listen("unix", mockSock)
	if err != nil {
		t.Fatal(err)
	}
	defer sockLn.Close()

	// Serve mock docker ping
	go func() {
		for {
			conn, err := sockLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				if req.URL.Path == "/_ping" {
					resp := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 2\r\n\r\nOK"
					_, _ = c.Write([]byte(resp))
				}
			}(conn)
		}
	}()

	// 2. Central server mock accepting the agent tunnel WebSocket
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	hub := agent.NewHub()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/agent/tunnel") {
			http.NotFound(w, r)
			return
		}
		name := r.URL.Query().Get("name")
		if r.URL.Query().Has("token") {
			t.Errorf("the token must not travel in the URL")
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != "secret-token" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}

		wsConn := agent.NewWSConn(ws)
		yConf := yamux.DefaultConfig()
		yConf.EnableKeepAlive = false
		yConf.LogOutput = io.Discard

		session, err := yamux.Server(wsConn, yConf)
		if err != nil {
			t.Errorf("yamux server error: %v", err)
			return
		}

		hub.Register(name, session)
		<-session.CloseChan()
		hub.Unregister(name, session)
	}))
	defer ts.Close()

	// 3. Start the agent client
	u, _ := url.Parse(ts.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := agent.Config{
		Server:   ts.URL,
		Token:    "secret-token",
		Name:     "test-worker",
		Socket:   mockSock,
		Insecure: true,
	}

	go func() {
		_ = agent.Run(ctx, cfg)
	}()

	// Wait for agent to register in hub
	connected := false
	for i := 0; i < 40; i++ {
		if hub.IsOnline("test-worker") {
			connected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !connected {
		t.Fatalf("agent failed to connect to hub within timeout")
	}

	// 4. Test dialing the agent through the hub
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()

	conn, err := hub.Dial(dialCtx, "test-worker")
	if err != nil {
		t.Fatalf("hub.Dial error: %v", err)
	}
	defer conn.Close()

	// Send an HTTP request to the mock docker daemon through the tunnel
	reqStr := fmt.Sprintf("GET /_ping HTTP/1.1\r\nHost: %s\r\n\r\n", u.Host)
	if _, err := conn.Write([]byte(reqStr)); err != nil {
		t.Fatalf("failed to write request: %v", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	if resp.StatusCode != http.StatusOK || string(body) != "OK" {
		t.Fatalf("unexpected response: status=%d, body=%q", resp.StatusCode, string(body))
	}

	// 5. Raw TCP to a port on the agent's host, and HTTP sniffing over it
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hi") }))
	defer web.Close()
	webAddr := strings.TrimPrefix(web.URL, "http://")
	if got := probe.Sniff(dialCtx, func(ctx context.Context) (net.Conn, error) { return hub.DialTarget(ctx, "test-worker", webAddr) }); got != "http" {
		t.Fatalf("Sniff through agent = %q, want http", got)
	}
	tc, err := hub.DialTarget(dialCtx, "test-worker", webAddr)
	if err != nil {
		t.Fatalf("DialTarget: %v", err)
	}
	fmt.Fprintf(tc, "GET / HTTP/1.0\r\nHost: x\r\n\r\n")
	r2, err := http.ReadResponse(bufio.NewReader(tc), nil)
	if err != nil {
		t.Fatalf("read through tunnel: %v", err)
	}
	b2, _ := io.ReadAll(r2.Body)
	tc.Close()
	if string(b2) != "hi" {
		t.Fatalf("body through tunnel = %q", b2)
	}

	// addresses outside the agent's host are refused
	if _, err := hub.DialTarget(dialCtx, "test-worker", "203.0.113.7:80"); err == nil || !strings.Contains(err.Error(), "not an address of this host") {
		t.Fatalf("expected refusal for a foreign address, got %v", err)
	}

	// the Docker proxy still works on the same session afterwards
	c2, err := hub.Dial(dialCtx, "test-worker")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	fmt.Fprintf(c2, "GET /_ping HTTP/1.1\r\nHost: x\r\n\r\n")
	if r3, err := http.ReadResponse(bufio.NewReader(c2), nil); err != nil || r3.StatusCode != 200 {
		t.Fatalf("docker proxy after dial: %v", err)
	}
}
