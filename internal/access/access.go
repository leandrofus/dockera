// Package access restricts who may talk to dockera by source address.
// Entries can be IPs, CIDRs or hostnames (re-resolved periodically, so
// dynamic DNS works).
package access

import (
	"context"
	"dockera/internal/i18n"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type List struct {
	mu       sync.RWMutex
	entries  []string
	prefixes []netip.Prefix
	hosts    map[string][]netip.Addr
}

// New builds a list. Loopback is always allowed.
func New(entries []string) (*List, error) {
	l := &List{}
	if err := l.Set(entries); err != nil {
		return nil, err
	}
	go l.refreshLoop()
	return l, nil
}

func (l *List) Set(entries []string) error {
	var prefixes []netip.Prefix
	hosts := map[string][]netip.Addr{}
	var clean []string
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		clean = append(clean, e)
		if e == "*" || e == "any" {
			prefixes = append(prefixes, netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0"))
			continue
		}
		if p, err := netip.ParsePrefix(e); err == nil {
			prefixes = append(prefixes, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(e); err == nil {
			prefixes = append(prefixes, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		if strings.ContainsAny(e, "/ ") {
			return i18n.Errorf("invalid allowlist entry: %q", e)
		}
		hosts[e] = resolve(e)
	}
	l.mu.Lock()
	l.entries, l.prefixes, l.hosts = clean, prefixes, hosts
	l.mu.Unlock()
	return nil
}

func (l *List) Entries() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]string(nil), l.entries...)
}

func resolve(host string) []netip.Addr {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		log.Printf("allowlist: could not resolve %s: %v", host, err)
		return nil
	}
	for i := range addrs {
		addrs[i] = addrs[i].Unmap()
	}
	return addrs
}

func (l *List) refreshLoop() {
	for range time.Tick(60 * time.Second) {
		l.mu.RLock()
		names := make([]string, 0, len(l.hosts))
		for h := range l.hosts {
			names = append(names, h)
		}
		l.mu.RUnlock()
		for _, h := range names {
			addrs := resolve(h)
			if addrs == nil {
				continue // keep last known addresses on transient failures
			}
			l.mu.Lock()
			if _, ok := l.hosts[h]; ok {
				l.hosts[h] = addrs
			}
			l.mu.Unlock()
		}
	}
}

func (l *List) Allowed(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsLoopback() {
		return true
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, p := range l.prefixes {
		if p.Contains(a) {
			return true
		}
	}
	for _, addrs := range l.hosts {
		for _, h := range addrs {
			if h == a {
				return true
			}
		}
	}
	return false
}

// RemoteIP extracts the client IP. X-Forwarded-For is only honoured when
// the direct peer is loopback and trustProxy is set.
func RemoteIP(r *http.Request, trustProxy bool) netip.Addr {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	ip := ap.Addr().Unmap()
	if trustProxy && ip.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if a, err := netip.ParseAddr(first); err == nil {
				return a.Unmap()
			}
		}
	}
	return ip
}

// Middleware rejects requests from addresses not on the list.
func (l *List) Middleware(trustProxy bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/agent/") {
			next.ServeHTTP(w, r)
			return
		}
		ip := RemoteIP(r, trustProxy)
		if !ip.IsValid() || !l.Allowed(ip) {
			log.Printf("access denied: %s %s %s", ip, r.Method, r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Listener drops connections from non-allowed peers right after accept,
// before any TLS handshake or HTTP parsing happens.
func (l *List) Listener(ln net.Listener) net.Listener {
	return &filteredListener{Listener: ln, list: l}
}

type filteredListener struct {
	net.Listener
	list *List
}

func (f *filteredListener) Accept() (net.Conn, error) {
	for {
		c, err := f.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if ap, err := netip.ParseAddrPort(c.RemoteAddr().String()); err == nil && f.list.Allowed(ap.Addr()) {
			return c, nil
		}
		log.Printf("connection refused from %s (not in the allowlist)", c.RemoteAddr())
		c.Close()
	}
}

// Match returns which entry allows a: "loopback", an IP/CIDR, a hostname,
// or "" when it is not allowed.
func (l *List) Match(a netip.Addr) string {
	a = a.Unmap()
	if a.IsLoopback() {
		return "loopback"
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, e := range l.entries {
		if p, err := netip.ParsePrefix(e); err == nil && p.Masked().Contains(a) {
			return e
		}
		if ip, err := netip.ParseAddr(e); err == nil && ip.Unmap() == a {
			return e
		}
		if e == "*" || e == "any" {
			return e
		}
		for _, h := range l.hosts[e] {
			if h == a {
				return e
			}
		}
	}
	return ""
}
