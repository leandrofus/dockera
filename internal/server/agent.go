package server

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"dockler/internal/agent"
	"dockler/internal/config"
)

var agentUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	CheckOrigin: func(r *http.Request) bool {
		return true // agents connect from any origin / IP
	},
}

func (s *Server) checkAgentToken(token string) bool {
	if token == "" {
		return false
	}
	cfg := s.Cfg.Snapshot()
	if cfg.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) == 1 {
		return true
	}
	for _, ep := range cfg.Endpoints {
		if ep.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(ep.Token)) == 1 {
			return true
		}
	}
	return false
}

func (s *Server) serveAgentBinary(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if !s.checkAgentToken(token) {
		http.Error(w, "invalid or missing token", http.StatusUnauthorized)
		return
	}

	exe, err := os.Executable()
	if err != nil {
		writeErr(w, err)
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="dockler-agent"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, exe)
}

func (s *Server) serveAgentInstallScript(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if !s.checkAgentToken(token) {
		http.Error(w, "invalid or missing token", http.StatusUnauthorized)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		name = "node-1"
	}

	scheme := "http"
	if s.Secure || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	serverURL := fmt.Sprintf("%s://%s", scheme, r.Host)
	if custom := r.URL.Query().Get("server"); custom != "" {
		serverURL = custom
	}

	script := fmt.Sprintf(`#!/bin/sh
# Dockler Agent Installer
set -e

SERVER="%s"
TOKEN="%s"
NAME="%s"

echo "============================================="
echo " Installing dockler-agent on $NAME"
echo " Central Server: $SERVER"
echo "============================================="

BIN_DEST="/usr/local/bin/dockler-agent"
if [ "$(id -u)" != "0" ]; then
    BIN_DEST="./dockler-agent"
fi

echo "Downloading standalone agent binary from $SERVER..."
if command -v curl >/dev/null 2>&1; then
    curl -kfsSL "$SERVER/api/agent/binary?token=$TOKEN" -o "$BIN_DEST"
elif command -v wget >/dev/null 2>&1; then
    wget --no-check-certificate -qO "$BIN_DEST" "$SERVER/api/agent/binary?token=$TOKEN"
else
    echo "Error: curl or wget is required to download dockler-agent." >&2
    exit 1
fi

chmod +x "$BIN_DEST"
echo "Binary downloaded to $BIN_DEST ✓"

if [ "$(id -u)" = "0" ] && command -v systemctl >/dev/null 2>&1; then
    echo "Installing systemd service (dockler-agent.service)..."
    cat <<EOF > /etc/systemd/system/dockler-agent.service
[Unit]
Description=dockler agent for Docker
Documentation=https://github.com/leandro/dockler
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
Type=simple
ExecStart=$BIN_DEST agent --server "$SERVER" --token "$TOKEN" --name "$NAME" --insecure
Restart=always
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable --now dockler-agent
    echo "============================================="
    echo " dockler-agent service is active and running! ✓"
    echo " Check status with: systemctl status dockler-agent"
    echo "============================================="
else
    echo "Starting agent in background..."
    nohup "$BIN_DEST" agent --server "$SERVER" --token "$TOKEN" --name "$NAME" --insecure > dockler-agent.log 2>&1 &
    PID=$!
    echo "============================================="
    echo " dockler-agent is running in background (PID: $PID) ✓"
    echo " Logs: dockler-agent.log"
    echo "============================================="
fi
`, serverURL, token, name)

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(script))
}

func (s *Server) handleAgentTunnel(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = r.Header.Get("X-Agent-Name")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		http.Error(w, "missing agent name", http.StatusBadRequest)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}

	// Validate agent token
	ep, found := s.Cfg.Endpoint(name)
	authorized := false
	if found {
		if ep.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(ep.Token)) == 1 {
			authorized = true
		} else if subtle.ConstantTimeCompare([]byte(token), []byte(s.Cfg.Token)) == 1 {
			authorized = true
		}
	} else if subtle.ConstantTimeCompare([]byte(token), []byte(s.Cfg.Token)) == 1 {
		// Auto-register endpoint if connecting with admin token
		authorized = true
		_ = s.Cfg.Update(func(cfg *config.Config) error {
			for _, e := range cfg.Endpoints {
				if e.Name == name {
					return nil
				}
			}
			cfg.Endpoints = append(cfg.Endpoints, config.Endpoint{
				Name:  name,
				URL:   "agent://" + name,
				Token: token,
			})
			return nil
		})
	}

	if !authorized {
		log.Printf("[agent] unauthorized connection attempt for %q from %s", name, r.RemoteAddr)
		http.Error(w, "unauthorized: invalid agent token", http.StatusUnauthorized)
		return
	}

	ws, err := agentUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[agent] upgrade failed for %q: %v", name, err)
		return
	}
	defer ws.Close()

	wsConn := agent.NewWSConn(ws)
	yamuxConf := yamux.DefaultConfig()
	yamuxConf.EnableKeepAlive = true
	yamuxConf.KeepAliveInterval = 15 * time.Second
	yamuxConf.LogOutput = log.Writer()

	session, err := yamux.Server(wsConn, yamuxConf)
	if err != nil {
		log.Printf("[agent] yamux server error for %q: %v", name, err)
		return
	}
	defer session.Close()

	agent.DefaultHub.Register(name, session)
	s.dropClient(name)
	log.Printf("[agent] node %q connected from %s ✓", name, r.RemoteAddr)

	<-session.CloseChan()

	agent.DefaultHub.Unregister(name, session)
	s.dropClient(name)
	log.Printf("[agent] node %q disconnected", name)
}
