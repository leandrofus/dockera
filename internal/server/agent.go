package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"dockera/internal/access"
	"dockera/internal/agent"
	"dockera/internal/config"
)

var agentUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	CheckOrigin: func(r *http.Request) bool {
		return true // agents connect from any origin / IP
	},
}

// bearer returns the token sent in the Authorization header.
func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// agentEndpoint returns the agent endpoint whose own token is token. The
// admin token is not accepted: a node keeps its token on disk, so it must
// only ever unlock that node.
func (s *Server) agentEndpoint(token string) (config.Endpoint, bool) {
	if token == "" {
		return config.Endpoint{}, false
	}
	for _, ep := range s.Cfg.Snapshot().Endpoints {
		if ep.Agent() && ep.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(ep.Token)) == 1 {
			if s.Cfg.CheckToken(token) {
				// registered by an earlier version with the admin token
				log.Printf("[agent] %q uses the admin token: give the host its own token in Settings → Hosts and reinstall the agent", ep.Name)
				return config.Endpoint{}, false
			}
			return ep, true
		}
	}
	return config.Endpoint{}, false
}

// agentPins returns the server certificate's sha256 fingerprint (hex, for
// `dockera agent --fingerprint`) and its public key pin (for curl
// --pinnedpubkey); both are empty without TLS.
func (s *Server) agentPins() (fp, pubkey string) {
	if len(s.TLSCert) == 0 {
		return "", ""
	}
	sum := sha256.Sum256(s.TLSCert)
	fp = hex.EncodeToString(sum[:])
	if cert, err := x509.ParseCertificate(s.TLSCert); err == nil {
		k := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		pubkey = "sha256//" + base64.StdEncoding.EncodeToString(k[:])
	}
	return fp, pubkey
}

// shellQuote quotes v for a POSIX shell.
func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'" }

// validServerURL accepts an absolute http(s) URL with a host.
func validServerURL(v string) bool {
	u, err := url.Parse(v)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.RawQuery == "" && u.Fragment == ""
}

// serveAgentBinary hands the dockera binary to an enrolled node (any agent
// token).
func (s *Server) serveAgentBinary(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.agentEndpoint(bearer(r)); !ok {
		http.Error(w, "invalid or missing token", http.StatusUnauthorized)
		return
	}

	exe, err := os.Executable()
	if err != nil {
		writeErr(w, err)
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="dockera-agent"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, exe)
}

// agentInstall returns the commands that enroll an agent host, for the
// admin UI. The token goes in a header (not in URLs that end up in proxy
// and access logs) and, with TLS, every download pins the server's key, so
// the very first contact is authenticated too.
func (s *Server) agentInstall(w http.ResponseWriter, r *http.Request) {
	ep, ok := s.Cfg.Endpoint(r.PathValue("name"))
	if !ok || !ep.Agent() || ep.Token == "" {
		writeErr(w, herr(http.StatusNotFound, "not an agent host: %s", r.PathValue("name")))
		return
	}
	server := strings.TrimRight(r.URL.Query().Get("server"), "/")
	if !validServerURL(server) {
		writeErr(w, badRequest("invalid server URL: %s", server))
		return
	}
	fp, pin := s.agentPins()
	curl := "curl -fsSL"
	if pin != "" {
		curl += " -k --pinnedpubkey " + shellQuote(pin)
	}
	auth := "-H " + shellQuote("Authorization: Bearer "+ep.Token)
	flags := "--server " + shellQuote(server) + " --name " + shellQuote(ep.Name)
	if fp != "" {
		flags += " --fingerprint " + fp
	}
	writeJSON(w, map[string]any{
		"tls": fp != "",
		"install": fmt.Sprintf("%s %s %s | sudo sh", curl, auth,
			shellQuote(server+"/api/agent/install.sh?server="+url.QueryEscape(server))),
		"manual": fmt.Sprintf("%s %s %s -o dockera-agent && chmod +x dockera-agent && sudo DOCKERA_AGENT_TOKEN=%s ./dockera-agent agent %s --install-service",
			curl, auth, shellQuote(server+"/api/agent/binary"), shellQuote(ep.Token), flags),
	})
}

// serveAgentInstallScript returns the installer run by `curl … | sudo sh`
// on a node; the token in the request decides which host it enrolls.
func (s *Server) serveAgentInstallScript(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)
	ep, ok := s.agentEndpoint(token)
	if !ok {
		http.Error(w, "invalid or missing token", http.StatusUnauthorized)
		return
	}

	scheme := "http"
	if s.Secure || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	serverURL := fmt.Sprintf("%s://%s", scheme, r.Host)
	if custom := strings.TrimRight(r.URL.Query().Get("server"), "/"); validServerURL(custom) {
		serverURL = custom
	}
	fp, pin := s.agentPins()
	curlTLS := ""
	if pin != "" {
		curlTLS = "-k --pinnedpubkey " + shellQuote(pin)
	}

	script := fmt.Sprintf(`#!/bin/sh
# dockera agent installer
set -eu

SERVER=%s
NAME=%s
FINGERPRINT=%s
TOKEN=%s

echo "Installing dockera-agent as $NAME (server: $SERVER)"
if [ "$(id -u)" != "0" ]; then
	echo "run it as root (| sudo sh): the agent is installed as a systemd service" >&2
	exit 1
fi
command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 1; }

# The token never goes on a command line or in the unit: it is kept in a
# root-only file read by the service.
install -d -m 0700 /etc/dockera-agent
(umask 077 && printf 'DOCKERA_AGENT_TOKEN=%%s\n' "$TOKEN" > /etc/dockera-agent/agent.env)

BIN=/usr/local/bin/dockera-agent
curl -fsSL %s -H "Authorization: Bearer $TOKEN" "$SERVER/api/agent/binary" -o "$BIN.new"
chmod 0755 "$BIN.new"
mv -f "$BIN.new" "$BIN"

FLAGS="--server $SERVER --name $NAME"
[ -n "$FINGERPRINT" ] && FLAGS="$FLAGS --fingerprint $FINGERPRINT"

if ! command -v systemctl >/dev/null 2>&1; then
	echo "systemd not found; start the agent with:"
	echo "  env \$(cat /etc/dockera-agent/agent.env) $BIN agent $FLAGS"
	exit 0
fi

cat > /etc/systemd/system/dockera-agent.service <<UNIT
[Unit]
Description=dockera agent
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
Type=simple
EnvironmentFile=/etc/dockera-agent/agent.env
ExecStart=$BIN agent $FLAGS
Restart=always
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable dockera-agent >/dev/null 2>&1
systemctl restart dockera-agent
echo "dockera-agent is running: systemctl status dockera-agent"
`, shellQuote(serverURL), shellQuote(ep.Name), shellQuote(fp), shellQuote(token), curlTLS)

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
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

	token := bearer(r)
	if token == "" {
		token = r.URL.Query().Get("token") // agents from earlier versions
	}

	// Only the host's own token opens its tunnel. Hosts (and their tokens)
	// are created from the UI or with `dockera endpoint add NAME agent://`.
	ep, authorized := s.agentEndpoint(token)
	if !authorized || ep.Name != name {
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
	agent.DefaultHub.SetAddr(name, access.RemoteIP(r, false).String())
	s.dropClient(name)
	log.Printf("[agent] node %q connected from %s ✓", name, r.RemoteAddr)

	<-session.CloseChan()

	agent.DefaultHub.Unregister(name, session)
	s.dropClient(name)
	log.Printf("[agent] node %q disconnected", name)
}
