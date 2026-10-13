// Package proxy exposes the local Docker Engine API over TCP, guarded by the
// allowlist and a bearer token. Compatible with the docker CLI
// (DOCKER_HOST=tcp://… plus an Authorization header in config.json) and
// with other dockler instances.
package proxy

import (
	"crypto/subtle"
	"log"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strings"

	"dockler/internal/docker"
)

type Options struct {
	Token    func() string
	ReadOnly bool
	// NoExec blocks exec/attach, the endpoints that give a shell.
	NoExec bool
}

var execPath = regexp.MustCompile(`^(/v[\d.]+)?/(containers/[^/]+/(exec|attach)|exec/)`)

func Handler(c *docker.Client, opt Options) http.Handler {
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = "docker"
			pr.Out.Host = "docker"
			pr.Out.Header.Del("Authorization")
		},
		Transport:     c.Transport(),
		FlushInterval: -1, // logs, events and pulls are streams
		ErrorLog:      log.Default(),
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !checkToken(r, opt.Token()) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"dockler: invalid or missing token (Authorization: Bearer <token>)"}`))
			return
		}
		if opt.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead && !strings.HasSuffix(r.URL.Path, "/_ping") {
			deny(w, "dockler: read-only proxy")
			return
		}
		if opt.NoExec && execPath.MatchString(r.URL.Path) {
			deny(w, "dockler: exec/attach disabled on this proxy")
			return
		}
		rp.ServeHTTP(w, r)
	})
}

func deny(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"message":"` + msg + `"}`))
}

func checkToken(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}
