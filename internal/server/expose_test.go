package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ducktainer/internal/access"
	"ducktainer/internal/config"
)

// gateFor builds the gate of an exposure whose container is a local
// httptest server echoing the cookies and the user header it receives.
func gateFor(t *testing.T, a config.ExposeAccess) (http.Handler, *exposeRT) {
	t.Helper()
	t.Setenv("DUCKTAINER_HOME", t.TempDir())
	t.Setenv("DUCKTAINER_KEY_FILE", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	acl, _ := access.New(nil)
	s := New(cfg, acl, os.DirFS(t.TempDir()))
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok cookie="+r.Header.Get("Cookie")+" user="+r.Header.Get("X-Ducktainer-User")+" xff="+r.Header.Get("X-Forwarded-For"))
	}))
	t.Cleanup(backend.Close)
	var d net.Dialer
	rt := &exposeRT{x: config.Exposure{ID: "x1", Kind: "quick", Access: a}, addr: backend.Listener.Addr().String(), at: time.Now().Add(time.Hour),
		dial: func(ctx context.Context, addr string) (net.Conn, error) { return d.DialContext(ctx, "tcp", addr) }}
	if err := rt.ips.Set(a.IPs); err != nil {
		t.Fatal(err)
	}
	return s.gate(rt), rt
}

func do(h http.Handler, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = "127.0.0.1:5555"
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestGatePublic(t *testing.T) {
	h, _ := gateFor(t, config.ExposeAccess{})
	w := do(h, "GET", "https://a.example/", map[string]string{"Cf-Connecting-IP": "203.0.113.9", "X-Ducktainer-User": "spoofed"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "xff=203.0.113.9") || strings.Contains(w.Body.String(), "spoofed") {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}

func TestGateIPs(t *testing.T) {
	h, _ := gateFor(t, config.ExposeAccess{IPs: []string{"198.51.100.0/24"}})
	if w := do(h, "GET", "https://a.example/", map[string]string{"Cf-Connecting-IP": "203.0.113.9"}); w.Code != 403 {
		t.Fatalf("outside the list: got %d", w.Code)
	}
	if w := do(h, "GET", "https://a.example/", map[string]string{"Cf-Connecting-IP": "198.51.100.20"}); w.Code != 200 {
		t.Fatalf("inside the list: got %d", w.Code)
	}
}

func TestGateLink(t *testing.T) {
	h, rt := gateFor(t, config.ExposeAccess{LinkKey: "sekret"})
	if w := do(h, "GET", "https://a.example/app", nil); w.Code != 403 {
		t.Fatalf("no key: got %d", w.Code)
	}
	if w := do(h, "GET", "https://a.example/app?dt_key=wrong", nil); w.Code != 403 {
		t.Fatalf("wrong key: got %d", w.Code)
	}
	w := do(h, "GET", "https://a.example/app?x=1&dt_key=sekret", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/app?x=1" {
		t.Fatalf("right key: got %d %q", w.Code, w.Header().Get("Location"))
	}
	cookie := strings.Split(w.Header().Get("Set-Cookie"), ";")[0]
	w = do(h, "GET", "https://a.example/app", map[string]string{"Cookie": cookie + "; app=1"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "cookie=app=1 ") {
		t.Fatalf("with cookie: got %d %q (the gate cookie must not reach the app)", w.Code, w.Body.String())
	}
	// a new key revokes the cookies of the old one
	rt.x.Access.LinkKey = "other"
	if w := do(h, "GET", "https://a.example/app", map[string]string{"Cookie": cookie}); w.Code != 403 {
		t.Fatalf("old cookie after a new key: got %d", w.Code)
	}
}

func TestGateLoginRedirect(t *testing.T) {
	h, _ := gateFor(t, config.ExposeAccess{Login: true})
	w := do(h, "GET", "https://a.example/app", map[string]string{"Accept": "text/html"})
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/.ducktainer/login?next=%2Fapp") {
		t.Fatalf("browser: got %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := do(h, "GET", "https://a.example/api", nil); w.Code != 401 {
		t.Fatalf("api client: got %d", w.Code)
	}
	if w := do(h, "GET", "https://a.example/.ducktainer/login", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `name="password"`) {
		t.Fatalf("login page: got %d", w.Code)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{"/app?x=1": "/app?x=1", "//evil.com": "/", "/\\evil.com": "/", "https://evil.com": "/", "": "/"} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
