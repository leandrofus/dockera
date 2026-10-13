package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ducktainer/internal/access"
	"ducktainer/internal/auth"
	"ducktainer/internal/cloudflare"
	"ducktainer/internal/config"
	"ducktainer/internal/i18n"
)

// Exposures publish container ports on the internet through Cloudflare.
// cloudflared runs here, as a child of ducktainer, and sends every request
// to a gate: a loopback reverse proxy per exposure that enforces who may
// pass (IPs/hosts, a ducktainer login, a secret link) before dialing the
// container the same way port forwards do (directly or through the agent).

const (
	gateCookie   = "__ducktainer"
	gatePrefix   = "/.ducktainer/"
	linkParam    = "dt_key"
	gateSession  = 12 * time.Hour
	targetMaxAge = 30 * time.Second
)

type exposeRT struct {
	mu    sync.Mutex
	x     config.Exposure
	url   string // public address
	state string // starting | connected | error message
	ips   access.List
	ln    net.Listener
	hs    *http.Server
	proc  *cloudflare.Proc

	// cached target address on the endpoint host
	addr string
	dial func(ctx context.Context, addr string) (net.Conn, error)
	at   time.Time
}

type exposures struct {
	mu        sync.Mutex
	rt        map[string]*exposeRT
	connector *cloudflare.Proc
	connState string
	allowed   sync.Map // id|user -> cachedAllow
}

type cachedAllow struct {
	ok bool
	at time.Time
}

// StartExposures brings back the persisted exposures (serve calls it once).
func (s *Server) StartExposures() {
	s.expo.mu.Lock()
	s.expo.rt = map[string]*exposeRT{}
	s.expo.mu.Unlock()
	for _, x := range s.Cfg.Snapshot().Exposures {
		if err := s.startExposure(x); err != nil {
			log.Printf("exposure %s: %v", x.ID, err)
		}
	}
	if err := s.syncTunnel(context.Background()); err != nil {
		log.Printf("cloudflare: %v", err)
	}
	go s.exposureLoop()
}

// StopExposures ends every cloudflared (on shutdown).
func (s *Server) StopExposures() {
	s.expo.mu.Lock()
	ids := make([]string, 0, len(s.expo.rt))
	for id := range s.expo.rt {
		ids = append(ids, id)
	}
	s.expo.mu.Unlock()
	for _, id := range ids {
		s.stopExposure(id)
	}
	s.setConnector(nil)
}

func (s *Server) exposureLoop() {
	for tick := 1; ; tick++ {
		time.Sleep(30 * time.Second)
		now := time.Now()
		for _, x := range s.Cfg.Snapshot().Exposures {
			if x.Expires != nil && now.After(*x.Expires) {
				log.Printf("exposure %s expired", x.ID)
				if err := s.removeExposure(context.Background(), x.ID); err != nil {
					log.Printf("exposure %s: %v", x.ID, err)
				}
			}
		}
		// re-resolve hostnames of IP allowlists (dynamic DNS)
		if tick%4 == 0 {
			s.expo.mu.Lock()
			for _, rt := range s.expo.rt {
				rt.ips.Set(rt.x.Access.IPs)
			}
			s.expo.mu.Unlock()
		}
	}
}

func (s *Server) startExposure(x config.Exposure) error {
	rt := &exposeRT{x: x, state: "starting"}
	if err := rt.ips.Set(x.Access.IPs); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	rt.ln = ln
	rt.hs = &http.Server{Handler: s.gate(rt), ReadHeaderTimeout: 30 * time.Second}
	go rt.hs.Serve(ln)
	switch x.Kind {
	case "quick":
		rt.proc = &cloudflare.Proc{
			Name: "quick " + x.ID,
			Args: []string{"tunnel", "--no-autoupdate", "--url", "http://" + ln.Addr().String()},
			OnURL: func(u string) {
				rt.mu.Lock()
				rt.url = u
				rt.mu.Unlock()
				log.Printf("exposure %s: %s/%s:%d at %s", x.ID, x.Endpoint, x.Container, x.Port, u)
			},
			OnState: func(st string) {
				rt.mu.Lock()
				if st == "starting" {
					rt.url = "" // a new process gets a new address
				}
				rt.state = st
				rt.mu.Unlock()
			},
		}
		rt.proc.Start()
	case "domain":
		rt.url = "https://" + x.Hostname
	}
	s.expo.mu.Lock()
	s.expo.rt[x.ID] = rt
	s.expo.mu.Unlock()
	return nil
}

func (s *Server) stopExposure(id string) {
	s.expo.mu.Lock()
	rt := s.expo.rt[id]
	delete(s.expo.rt, id)
	s.expo.mu.Unlock()
	if rt == nil {
		return
	}
	if rt.proc != nil {
		rt.proc.Stop()
	}
	rt.hs.Close()
}

// removeExposure stops it, drops its DNS record and forgets it.
func (s *Server) removeExposure(ctx context.Context, id string) error {
	var x *config.Exposure
	for _, e := range s.Cfg.Snapshot().Exposures {
		if e.ID == id {
			x = &e
		}
	}
	if x == nil {
		return herr(http.StatusNotFound, "no such exposure")
	}
	s.stopExposure(id)
	err := s.Cfg.Update(func(c *config.Config) error {
		c.Exposures = slices.DeleteFunc(c.Exposures, func(e config.Exposure) bool { return e.ID == id })
		return nil
	})
	if err != nil {
		return err
	}
	if x.Kind == "domain" {
		if cf := s.Cfg.Snapshot().Cloudflare; cf != nil && cf.APIToken != "" && x.DNSRecord != "" {
			api := &cloudflare.API{Token: cf.APIToken}
			if err := api.DeleteDNS(ctx, x.ZoneID, x.DNSRecord); err != nil {
				log.Printf("exposure %s: removing DNS record: %v", id, err)
			}
		}
		return s.syncTunnel(ctx)
	}
	return nil
}

// syncTunnel pushes the routes of the domain exposures to the account's
// tunnel and runs its connector while there is something to serve.
func (s *Server) syncTunnel(ctx context.Context) error {
	cf := s.Cfg.Snapshot().Cloudflare
	var rules []cloudflare.Ingress
	s.expo.mu.Lock()
	for _, rt := range s.expo.rt {
		if rt.x.Kind == "domain" {
			rules = append(rules, cloudflare.Ingress{Hostname: rt.x.Hostname, Service: "http://" + rt.ln.Addr().String()})
		}
	}
	s.expo.mu.Unlock()
	sort.Slice(rules, func(i, j int) bool { return rules[i].Hostname < rules[j].Hostname })
	if cf == nil || cf.TunnelID == "" {
		s.setConnector(nil)
		if len(rules) > 0 {
			return i18n.Errorf("there are exposures on your domains but no Cloudflare account is connected")
		}
		return nil
	}
	api := &cloudflare.API{Token: cf.APIToken}
	if err := api.PutIngress(ctx, cf.AccountID, cf.TunnelID, rules); err != nil {
		return err
	}
	if len(rules) == 0 {
		s.setConnector(nil)
		return nil
	}
	s.setConnector(cf)
	return nil
}

func (s *Server) setConnector(cf *config.Cloudflare) {
	s.expo.mu.Lock()
	old := s.expo.connector
	if cf != nil && old != nil {
		s.expo.mu.Unlock()
		return // already running
	}
	s.expo.connector = nil
	if cf != nil {
		p := &cloudflare.Proc{Name: "connector " + cf.TunnelName, Args: []string{"tunnel", "--no-autoupdate", "run"}, Env: []string{"TUNNEL_TOKEN=" + cf.TunnelToken}}
		p.OnState = func(st string) {
			s.expo.mu.Lock()
			s.expo.connState = st
			s.expo.mu.Unlock()
		}
		s.expo.connector = p
		p.Start()
	} else {
		s.expo.connState = ""
	}
	s.expo.mu.Unlock()
	if old != nil {
		old.Stop()
	}
}

// ---- reaching the container ----

type ctrPorts struct {
	Name  string `json:"Name"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// resolveTarget finds where the exposed port answers: the published port
// when there is one, the container's IP otherwise (local and agent hosts).
// The container is looked up by name, so recreating it keeps the exposure.
func (s *Server) resolveTarget(ctx context.Context, ep, container string, port int) (string, func(context.Context, string) (net.Conn, error), error) {
	tgt := s.jumpTarget(ep)
	if tgt.dial == nil {
		return "", nil, i18n.Errorf("this host cannot be tunneled")
	}
	c, err := s.Client(ep)
	if err != nil {
		return "", nil, err
	}
	var in ctrPorts
	if err := c.Get(ctx, "/containers/"+url.PathEscape(container)+"/json", nil, &in); err != nil {
		return "", nil, err
	}
	if !in.State.Running {
		return "", nil, i18n.Errorf("the container %s is not running", container)
	}
	for _, b := range in.NetworkSettings.Ports[strconv.Itoa(port)+"/tcp"] {
		if b.HostPort == "" {
			continue
		}
		h := "127.0.0.1"
		if !tgt.local {
			h = tgt.host
		}
		if ip, err := netip.ParseAddr(b.HostIP); err == nil && !ip.IsUnspecified() {
			h = ip.String()
		}
		return net.JoinHostPort(h, b.HostPort), tgt.dial, nil
	}
	if !tgt.local {
		return "", nil, i18n.Errorf("internal ports can only be tunneled on local and agent hosts")
	}
	names := make([]string, 0, len(in.NetworkSettings.Networks))
	for n := range in.NetworkSettings.Networks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if ip := in.NetworkSettings.Networks[n].IPAddress; ip != "" {
			return net.JoinHostPort(ip, strconv.Itoa(port)), tgt.dial, nil
		}
	}
	return "", nil, i18n.Errorf("the container has no IP address (is it running?)")
}

func (rt *exposeRT) dialTarget(s *Server, ctx context.Context) (net.Conn, error) {
	rt.mu.Lock()
	addr, dial, fresh := rt.addr, rt.dial, time.Since(rt.at) < targetMaxAge
	x := rt.x
	rt.mu.Unlock()
	if !fresh || dial == nil {
		var err error
		if addr, dial, err = s.resolveTarget(ctx, x.Endpoint, x.Container, x.Port); err != nil {
			return nil, err
		}
		rt.mu.Lock()
		rt.addr, rt.dial, rt.at = addr, dial, time.Now()
		rt.mu.Unlock()
	}
	conn, err := dial(ctx, addr)
	if err != nil {
		rt.mu.Lock()
		rt.at = time.Time{} // look it up again next time (recreated container)
		rt.mu.Unlock()
	}
	return conn, err
}

// ---- the gate ----

type gateClaims struct {
	X   string `json:"x"`           // exposure
	U   string `json:"u,omitempty"` // system user (login)
	K   string `json:"k,omitempty"` // link key fingerprint (link)
	Exp int64  `json:"e"`
}

func (s *Server) gateMAC(p string) string {
	m := hmac.New(sha256.New, []byte("gate:"+s.Cfg.Snapshot().SessionSecret))
	m.Write([]byte(p))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Server) signGate(c gateClaims) string {
	data, _ := json.Marshal(c)
	p := base64.RawURLEncoding.EncodeToString(data)
	return p + "." + s.gateMAC(p)
}

func (s *Server) verifyGate(v string) (*gateClaims, bool) {
	p, sig, ok := strings.Cut(v, ".")
	if !ok || subtle.ConstantTimeCompare([]byte(sig), []byte(s.gateMAC(p))) != 1 {
		return nil, false
	}
	data, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return nil, false
	}
	var c gateClaims
	if json.Unmarshal(data, &c) != nil || time.Now().Unix() > c.Exp {
		return nil, false
	}
	return &c, true
}

func keyPrint(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:6])
}

// userAllowed: the listed users and groups, or anyone with a ducktainer
// role when the lists are empty. Cached for a few minutes, like roles.
func (s *Server) userAllowed(x config.Exposure, user string) bool {
	key := x.ID + "|" + user
	if v, ok := s.expo.allowed.Load(key); ok && time.Since(v.(cachedAllow).at) < 5*time.Minute {
		return v.(cachedAllow).ok
	}
	ok := false
	a := x.Access
	switch {
	case user == "root" && !s.Cfg.Snapshot().Auth.AllowRoot:
	case len(a.Users) == 0 && len(a.Groups) == 0:
		_, err := s.currentRole(user)
		ok = err == nil
	case slices.Contains(a.Users, user):
		ok = true
	default:
		if groups, err := auth.Groups(user); err == nil {
			ok = slices.ContainsFunc(groups, func(g string) bool { return slices.Contains(a.Groups, g) })
		}
	}
	s.expo.allowed.Store(key, cachedAllow{ok, time.Now()})
	return ok
}

func gateClient(r *http.Request) netip.Addr {
	// cloudflared connects from loopback; Cloudflare sets the real client
	if a, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("Cf-Connecting-IP"))); err == nil {
		return a.Unmap()
	}
	return access.RemoteIP(r, false)
}

func (s *Server) gate(rt *exposeRT) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			rt.mu.Lock()
			scheme := rt.x.Scheme
			rt.mu.Unlock()
			if scheme != "https" {
				scheme = "http"
			}
			pr.Out.URL.Scheme, pr.Out.URL.Host = scheme, "container"
			pr.Out.Host = pr.In.Host // apps build links from it
			client := gateClient(pr.In).String()
			pr.Out.Header.Set("X-Forwarded-For", client)
			pr.Out.Header.Set("X-Real-IP", client)
			pr.Out.Header.Set("X-Forwarded-Host", pr.In.Host)
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
			pr.Out.Header.Del("X-Ducktainer-User")
			if u, _ := pr.In.Context().Value(gateUserKey{}).(string); u != "" {
				pr.Out.Header.Set("X-Ducktainer-User", u)
			}
			stripGateCookie(pr.Out)
		},
		Transport: &http.Transport{
			DialContext:           func(ctx context.Context, _, _ string) (net.Conn, error) { return rt.dialTarget(s, ctx) },
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, // containers use self-signed certs
			ForceAttemptHTTP2:     false,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 5 * time.Minute,
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("exposure %s: %v", rt.x.ID, err)
			s.gatePage(w, r, http.StatusBadGateway, "Not available", i18n.T(gateLang(r), "The service is not answering right now."), nil)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rt.mu.Lock()
		x := rt.x
		rt.mu.Unlock()
		lang := gateLang(r)
		client := gateClient(r)
		if len(x.Access.IPs) > 0 && !rt.ips.Allowed(client) {
			log.Printf("exposure %s: %s denied (not in the allowlist)", x.ID, client)
			s.gatePage(w, r, http.StatusForbidden, "Access denied", i18n.T(lang, "Your address (%s) is not allowed.", client), nil)
			return
		}
		if strings.HasPrefix(r.URL.Path, gatePrefix) {
			switch strings.TrimPrefix(r.URL.Path, gatePrefix) {
			case "login":
				s.gateLogin(w, r, x)
				return
			case "logout":
				http.SetCookie(w, &http.Cookie{Name: gateCookie, Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true})
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}
		if x.Access.Login || x.Access.LinkKey != "" {
			user, ok := s.gateIdentity(w, r, x)
			if !ok {
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), gateUserKey{}, user))
		}
		proxy.ServeHTTP(w, r)
	})
}

type gateUserKey struct{}

// gateIdentity checks the gate cookie or the link key; on failure it has
// already answered (login page, redirect or 403).
func (s *Server) gateIdentity(w http.ResponseWriter, r *http.Request, x config.Exposure) (string, bool) {
	if k := r.URL.Query().Get(linkParam); k != "" && x.Access.LinkKey != "" {
		if subtle.ConstantTimeCompare([]byte(k), []byte(x.Access.LinkKey)) == 1 {
			s.setGateCookie(w, gateClaims{X: x.ID, K: keyPrint(x.Access.LinkKey)})
			q := r.URL.Query()
			q.Del(linkParam)
			u := *r.URL
			u.RawQuery = q.Encode()
			http.Redirect(w, r, u.RequestURI(), http.StatusSeeOther)
			return "", false
		}
	}
	if c, err := r.Cookie(gateCookie); err == nil {
		if cl, ok := s.verifyGate(c.Value); ok && cl.X == x.ID {
			switch {
			case cl.K != "" && x.Access.LinkKey != "" && cl.K == keyPrint(x.Access.LinkKey):
				return "", true
			case cl.U != "" && x.Access.Login && s.userAllowed(x, cl.U):
				return cl.U, true
			}
		}
	}
	if !x.Access.Login {
		s.gatePage(w, r, http.StatusForbidden, "Access denied", i18n.T(gateLang(r), "Open the link you were given to get in."), nil)
		return "", false
	}
	// browsers go to the login page; API clients get a plain 401
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, gatePrefix+"login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
	} else {
		http.Error(w, "authentication required", http.StatusUnauthorized)
	}
	return "", false
}

func (s *Server) setGateCookie(w http.ResponseWriter, c gateClaims) {
	exp := time.Now().Add(gateSession)
	c.Exp = exp.Unix()
	http.SetCookie(w, &http.Cookie{Name: gateCookie, Value: s.signGate(c), Path: "/", Expires: exp, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

func stripGateCookie(r *http.Request) {
	cs := r.Cookies()
	r.Header.Del("Cookie")
	for _, c := range cs {
		if c.Name != gateCookie {
			r.AddCookie(c)
		}
	}
}

func safeNext(n string) string {
	if !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") || strings.HasPrefix(n, "/\\") {
		return "/"
	}
	return n
}

func (s *Server) gateLogin(w http.ResponseWriter, r *http.Request, x config.Exposure) {
	lang := gateLang(r)
	if !x.Access.Login {
		http.NotFound(w, r)
		return
	}
	next := safeNext(r.FormValue("next"))
	form := map[string]string{"next": next}
	if r.Method != http.MethodPost {
		s.gatePage(w, r, http.StatusOK, "Sign in", "", form)
		return
	}
	user, pass := strings.TrimSpace(r.FormValue("username")), r.FormValue("password")
	ip := gateClient(r).String()
	keys := []string{"ip:" + ip, "user:" + user}
	for _, k := range keys {
		if blocked, wait := s.limiter.Blocked(k); blocked {
			form["error"] = i18n.T(lang, "too many failed attempts; wait %d minutes", int(wait.Minutes())+1)
			s.gatePage(w, r, http.StatusTooManyRequests, "Sign in", "", form)
			return
		}
	}
	snap := s.Cfg.Snapshot()
	var err error
	switch {
	case user == "" || pass == "":
		err = i18n.Errorf("wrong username or password")
	case !snap.Auth.PAM:
		err = i18n.Errorf("system-user access is disabled")
	case user == "root" && !snap.Auth.AllowRoot:
		err = i18n.Errorf("root login is disabled")
	default:
		err = auth.Login(snap.Auth.PAMService, user, pass, ip)
	}
	if err == nil {
		s.expo.allowed.Delete(x.ID + "|" + user)
		if !s.userAllowed(x, user) {
			err = i18n.Errorf("%s may not open this service", user)
		}
	}
	entry := auditEntry{Time: time.Now().UTC().Format(time.RFC3339), User: user, Method: "LOGIN", Path: "expose:" + x.ID, Status: 200, IP: ip}
	if err != nil {
		for _, k := range keys {
			s.limiter.Fail(k)
		}
		time.Sleep(600 * time.Millisecond)
		entry.Status, entry.Note = http.StatusUnauthorized, err.Error()
		s.audit.write(entry)
		// strangers only learn that it did not work; the audit log has why
		form["error"] = i18n.T(lang, "wrong username or password")
		if errors.Is(err, auth.ErrUnavailable) {
			form["error"] = i18n.Render(err, lang)
		}
		form["username"] = user
		s.gatePage(w, r, http.StatusUnauthorized, "Sign in", "", form)
		return
	}
	for _, k := range keys {
		s.limiter.Reset(k)
	}
	s.audit.write(entry)
	s.setGateCookie(w, gateClaims{X: x.ID, U: user})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func gateLang(r *http.Request) string { return i18n.Pick("", r.Header.Get("Accept-Language")) }

var gateTmpl = template.Must(template.New("gate").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex"><title>{{.Title}}</title>
<style>
:root{--bg:#f6f7f9;--card:#fff;--fg:#1d2330;--muted:#677084;--line:#dde1e8;--accent:#e8a317;--bad:#c2410c}
@media (prefers-color-scheme:dark){:root{--bg:#12151b;--card:#1b2029;--fg:#e7eaf0;--muted:#98a1b3;--line:#2c3340;--bad:#fb923c}}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,sans-serif;padding:16px}
.card{width:100%;max-width:360px;background:var(--card);border:1px solid var(--line);border-radius:12px;padding:28px}
h1{font-size:19px;margin:0 0 6px}p{color:var(--muted);margin:0 0 18px}label{display:block;font-size:13px;color:var(--muted);margin:12px 0 4px}
input{width:100%;padding:9px 11px;border:1px solid var(--line);border-radius:8px;background:transparent;color:inherit;font:inherit}
button{margin-top:18px;width:100%;padding:10px;border:0;border-radius:8px;background:var(--accent);color:#1d2330;font:600 15px system-ui,sans-serif;cursor:pointer}
.err{color:var(--bad);font-size:14px;margin:12px 0 0}.foot{margin-top:18px;font-size:12px;color:var(--muted);text-align:center}
</style></head><body><main class="card">
<h1>{{.Title}}</h1>{{if .Message}}<p>{{.Message}}</p>{{end}}
{{if .Form}}<form method="post" action="/.ducktainer/login">
<input type="hidden" name="next" value="{{index .Form "next"}}">
<label for="u">{{.User}}</label><input id="u" name="username" autocomplete="username" value="{{index .Form "username"}}" required autofocus>
<label for="p">{{.Pass}}</label><input id="p" name="password" type="password" autocomplete="current-password" required>
{{with index .Form "error"}}<div class="err">{{.}}</div>{{end}}
<button type="submit">{{.Submit}}</button></form>{{end}}
<div class="foot">{{.Host}} · ducktainer</div>
</main></body></html>`))

func (s *Server) gatePage(w http.ResponseWriter, r *http.Request, status int, title, msg string, form map[string]string) {
	lang := gateLang(r)
	if form != nil && msg == "" {
		msg = i18n.T(lang, "Sign in with your system account to continue.")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(status)
	gateTmpl.Execute(w, map[string]any{
		"Lang": lang, "Title": i18n.T(lang, title), "Message": msg, "Form": form, "Host": r.Host,
		"User": i18n.T(lang, "Username"), "Pass": i18n.T(lang, "Password"), "Submit": i18n.T(lang, "Sign in"),
	})
}

// ---- API ----

type exposureView struct {
	config.Exposure
	URL     string `json:"url"`
	State   string `json:"state"`
	Link    string `json:"link,omitempty"` // URL carrying the key (admins)
	HasLink bool   `json:"has_link"`
}

func (s *Server) exposureViews(admin bool) []exposureView {
	out := []exposureView{}
	snap := s.Cfg.Snapshot()
	s.expo.mu.Lock()
	connState := s.expo.connState
	rts := s.expo.rt
	s.expo.mu.Unlock()
	for _, x := range snap.Exposures {
		v := exposureView{Exposure: x, HasLink: x.Access.LinkKey != "", State: "stopped"}
		v.Access.LinkKey = ""
		if rt := rts[x.ID]; rt != nil {
			rt.mu.Lock()
			v.URL, v.State = rt.url, rt.state
			rt.mu.Unlock()
		}
		if x.Kind == "domain" {
			v.State = connState
			if v.State == "" {
				v.State = "stopped"
			}
		}
		if admin && v.HasLink && v.URL != "" {
			v.Link = v.URL + "/?" + linkParam + "=" + url.QueryEscape(x.Access.LinkKey)
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

func (s *Server) listExposures(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.exposureViews(sessionFrom(r.Context()).Role == auth.RoleAdmin))
}

type exposeAccessBody struct {
	IPs    []string `json:"ips"`
	Login  bool     `json:"login"`
	Users  []string `json:"users"`
	Groups []string `json:"groups"`
	Link   bool     `json:"link"`
	// NewLink replaces the key (old links stop working)
	NewLink bool `json:"new_link"`
}

func cleanList(in []string) []string {
	var out []string
	for _, v := range in {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func (s *Server) buildAccess(b exposeAccessBody, prev config.ExposeAccess) (config.ExposeAccess, error) {
	a := config.ExposeAccess{IPs: cleanList(b.IPs), Login: b.Login, Users: cleanList(b.Users), Groups: cleanList(b.Groups)}
	var probe access.List
	if err := probe.Set(a.IPs); err != nil {
		return a, badRequest(err.Error())
	}
	if a.Login && !s.Cfg.Snapshot().Auth.PAM {
		return a, badRequest("signing in needs system users, which are disabled in Settings → Access")
	}
	if !a.Login {
		a.Users, a.Groups = nil, nil
	}
	if b.Link {
		a.LinkKey = prev.LinkKey
		if a.LinkKey == "" || b.NewLink {
			k := make([]byte, 18)
			rand.Read(k)
			a.LinkKey = base64.RawURLEncoding.EncodeToString(k)
		}
	}
	return a, nil
}

var hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func (s *Server) createExposure(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Container  string           `json:"container"`
		Port       int              `json:"port"`
		Scheme     string           `json:"scheme"`
		Kind       string           `json:"kind"`
		Subdomain  string           `json:"subdomain"`
		ZoneID     string           `json:"zone_id"`
		ExpiresMin int              `json:"expires_minutes"`
		Access     exposeAccessBody `json:"access"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	epName := r.PathValue("ep")
	body.Container = strings.TrimPrefix(strings.TrimSpace(body.Container), "/")
	if body.Container == "" || body.Port < 1 || body.Port > 65535 {
		writeErr(w, badRequest("pick a container and a port"))
		return
	}
	acc, err := s.buildAccess(body.Access, config.ExposeAccess{})
	if err != nil {
		writeErr(w, err)
		return
	}
	// check the port answers before publishing anything
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	addr, dial, err := s.resolveTarget(ctx, epName, body.Container, body.Port)
	if err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	conn, err := dial(ctx, addr)
	if err != nil {
		writeErr(w, herr(http.StatusBadGateway, "cannot reach %s: %v", addr, err))
		return
	}
	conn.Close()

	id := make([]byte, 6)
	rand.Read(id)
	x := config.Exposure{
		ID: hex.EncodeToString(id), Kind: body.Kind, Endpoint: epName, Container: body.Container, Port: body.Port,
		Access: acc, User: sessionFrom(r.Context()).User, Created: time.Now().UTC(),
	}
	if body.Scheme == "https" {
		x.Scheme = "https"
	}
	if body.ExpiresMin > 0 {
		t := time.Now().Add(time.Duration(body.ExpiresMin) * time.Minute).UTC()
		x.Expires = &t
	}
	cf := s.Cfg.Snapshot().Cloudflare
	switch body.Kind {
	case "quick":
	case "domain":
		if cf == nil || cf.TunnelID == "" {
			writeErr(w, badRequest("connect a Cloudflare account first (Settings → Cloudflare)"))
			return
		}
		sub := strings.ToLower(strings.Trim(strings.TrimSpace(body.Subdomain), "."))
		for _, l := range strings.Split(sub, ".") {
			if sub != "" && !hostLabel.MatchString(l) {
				writeErr(w, badRequest("invalid name: %s", sub))
				return
			}
		}
		api := &cloudflare.API{Token: cf.APIToken}
		zones, err := api.Zones(ctx, cf.AccountID)
		if err != nil {
			writeErr(w, err)
			return
		}
		i := slices.IndexFunc(zones, func(z cloudflare.Zone) bool { return z.ID == body.ZoneID })
		if i < 0 {
			writeErr(w, badRequest("pick one of the account's domains"))
			return
		}
		x.ZoneID, x.Hostname = body.ZoneID, zones[i].Name
		if sub != "" {
			x.Hostname = sub + "." + zones[i].Name
		}
		for _, e := range s.Cfg.Snapshot().Exposures {
			if e.Hostname == x.Hostname {
				writeErr(w, badRequest("%s is already exposed", x.Hostname))
				return
			}
		}
		if x.DNSRecord, err = api.RouteDNS(ctx, x.ZoneID, x.Hostname, cf.TunnelID); err != nil {
			writeErr(w, err)
			return
		}
	default:
		writeErr(w, badRequest("unknown kind: %s", body.Kind))
		return
	}
	if err := s.Cfg.Update(func(c *config.Config) error { c.Exposures = append(c.Exposures, x); return nil }); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.startExposure(x); err != nil {
		s.removeExposure(context.Background(), x.ID)
		writeErr(w, err)
		return
	}
	if x.Kind == "domain" {
		if err := s.syncTunnel(ctx); err != nil {
			s.removeExposure(context.Background(), x.ID)
			writeErr(w, err)
			return
		}
	} else {
		// wait a little for the trycloudflare.com address
		for deadline := time.Now().Add(25 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
			s.expo.mu.Lock()
			rt := s.expo.rt[x.ID]
			s.expo.mu.Unlock()
			rt.mu.Lock()
			done := rt.url != "" || (rt.state != "starting" && rt.state != "connected")
			rt.mu.Unlock()
			if done || r.Context().Err() != nil {
				break
			}
		}
	}
	log.Printf("exposure %s: %s %s/%s:%d by %s", x.ID, x.Kind, epName, x.Container, x.Port, x.User)
	for _, v := range s.exposureViews(true) {
		if v.ID == x.ID {
			writeJSON(w, v)
			return
		}
	}
	writeJSON(w, x)
}

func (s *Server) updateExposureAccess(w http.ResponseWriter, r *http.Request) {
	var body exposeAccessBody
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	id := r.PathValue("id")
	i := slices.IndexFunc(s.Cfg.Snapshot().Exposures, func(e config.Exposure) bool { return e.ID == id })
	if i < 0 {
		writeErr(w, herr(http.StatusNotFound, "no such exposure"))
		return
	}
	a, err := s.buildAccess(body, s.Cfg.Snapshot().Exposures[i].Access)
	if err != nil {
		writeErr(w, err)
		return
	}
	var updated *config.Exposure
	err = s.Cfg.Update(func(c *config.Config) error {
		for i := range c.Exposures {
			if c.Exposures[i].ID == id {
				c.Exposures[i].Access = a
				x := c.Exposures[i]
				updated = &x
				return nil
			}
		}
		return herr(http.StatusNotFound, "no such exposure")
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	s.expo.mu.Lock()
	rt := s.expo.rt[id]
	s.expo.mu.Unlock()
	if rt != nil {
		rt.mu.Lock()
		rt.x = *updated
		rt.mu.Unlock()
		rt.ips.Set(updated.Access.IPs)
	}
	s.expo.allowed.Clear()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) deleteExposure(w http.ResponseWriter, r *http.Request) {
	if err := s.removeExposure(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- Cloudflare account ----

func (s *Server) cloudflareInfo(w http.ResponseWriter, r *http.Request) {
	cf := s.Cfg.Snapshot().Cloudflare
	s.expo.mu.Lock()
	conn := s.expo.connState
	s.expo.mu.Unlock()
	out := map[string]any{"binary": cloudflare.Binary(), "version": cloudflare.Version, "connector": conn, "pam": s.Cfg.Snapshot().Auth.PAM}
	if cf != nil && cf.TunnelID != "" {
		out["connected"] = true
		out["account_id"], out["account_name"] = cf.AccountID, cf.AccountName
		out["tunnel_id"], out["tunnel_name"] = cf.TunnelID, cf.TunnelName
		if r.URL.Query().Get("zones") == "1" {
			zones, err := (&cloudflare.API{Token: cf.APIToken}).Zones(r.Context(), cf.AccountID)
			if err != nil {
				out["zones_error"] = i18n.Render(err, reqLang(r))
			}
			if zones == nil {
				zones = []cloudflare.Zone{}
			}
			out["zones"] = zones
		}
	}
	writeJSON(w, out)
}

func (s *Server) connectCloudflare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIToken  string `json:"api_token"`
		AccountID string `json:"account_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	api := &cloudflare.API{Token: strings.TrimSpace(body.APIToken)}
	if api.Token == "" {
		writeErr(w, badRequest("paste an API token"))
		return
	}
	accounts, err := api.Accounts(r.Context())
	if err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	var acc *cloudflare.Account
	for i := range accounts {
		if accounts[i].ID == body.AccountID || (body.AccountID == "" && len(accounts) == 1) {
			acc = &accounts[i]
		}
	}
	if acc == nil {
		if len(accounts) == 0 {
			writeErr(w, badRequest("the token cannot see any account"))
			return
		}
		writeJSON(w, map[string]any{"choose": accounts}) // the UI asks which one
		return
	}
	host, _ := os.Hostname()
	name := "ducktainer-" + strings.ToLower(strings.Split(host, ".")[0])
	tun, err := api.EnsureTunnel(r.Context(), acc.ID, name)
	if err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	tok, err := api.TunnelToken(r.Context(), acc.ID, tun.ID)
	if err != nil {
		writeErr(w, badRequest(err.Error()))
		return
	}
	if _, err := api.Zones(r.Context(), acc.ID); err != nil {
		writeErr(w, badRequest("the token cannot list the account's domains: %v", err))
		return
	}
	cf := &config.Cloudflare{APIToken: api.Token, AccountID: acc.ID, AccountName: acc.Name, TunnelID: tun.ID, TunnelName: tun.Name, TunnelToken: tok}
	if err := s.Cfg.Update(func(c *config.Config) error { c.Cloudflare = cf; return nil }); err != nil {
		writeErr(w, err)
		return
	}
	s.setConnector(nil) // the token may belong to a different tunnel now
	if err := s.syncTunnel(r.Context()); err != nil {
		writeErr(w, err)
		return
	}
	go cloudflare.Ensure(context.Background()) // have the binary ready
	writeJSON(w, map[string]any{"ok": true, "tunnel_name": tun.Name})
}

// disconnectCloudflare removes the domain exposures (and their DNS records),
// the tunnel and the stored credentials.
func (s *Server) disconnectCloudflare(w http.ResponseWriter, r *http.Request) {
	snap := s.Cfg.Snapshot()
	for _, x := range snap.Exposures {
		if x.Kind == "domain" {
			if err := s.removeExposure(r.Context(), x.ID); err != nil {
				log.Printf("exposure %s: %v", x.ID, err)
			}
		}
	}
	s.setConnector(nil)
	if cf := snap.Cloudflare; cf != nil && cf.TunnelID != "" {
		if err := (&cloudflare.API{Token: cf.APIToken}).DeleteTunnel(r.Context(), cf.AccountID, cf.TunnelID); err != nil {
			log.Printf("cloudflare: deleting tunnel %s: %v", cf.TunnelName, err)
		}
	}
	if err := s.Cfg.Update(func(c *config.Config) error { c.Cloudflare = nil; return nil }); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
