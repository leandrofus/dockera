package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ducktainer/internal/i18n"
)

// API is a minimal Cloudflare v4 client. The token needs Account › Cloudflare
// Tunnel › Edit and Zone › DNS › Edit (plus Zone › Zone › Read to list zones).
type API struct {
	Token string
	Base  string // tests; default https://api.cloudflare.com/client/v4
}

type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Tunnel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment,omitempty"`
}

// Ingress is one route of a remotely managed tunnel.
type Ingress struct {
	Hostname      string         `json:"hostname,omitempty"`
	Service       string         `json:"service"`
	OriginRequest map[string]any `json:"originRequest,omitempty"`
}

var client = &http.Client{Timeout: 30 * time.Second}

func (a *API) do(ctx context.Context, method, path string, body, out any) error {
	base := a.Base
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	var rd io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return i18n.Errorf("Cloudflare API: %v", err)
	}
	defer resp.Body.Close()
	var env struct {
		Success bool `json:"success"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&env); err != nil {
		return i18n.Errorf("Cloudflare API: %s", resp.Status)
	}
	if !env.Success {
		msgs := make([]string, 0, len(env.Errors))
		for _, e := range env.Errors {
			msgs = append(msgs, fmt.Sprintf("%s (%d)", e.Message, e.Code))
		}
		if len(msgs) == 0 {
			msgs = append(msgs, resp.Status)
		}
		return i18n.Errorf("Cloudflare API: %s", strings.Join(msgs, "; "))
	}
	if out != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func (a *API) Accounts(ctx context.Context) ([]Account, error) {
	var out []Account
	err := a.do(ctx, http.MethodGet, "/accounts?per_page=50", nil, &out)
	return out, err
}

func (a *API) Zones(ctx context.Context, account string) ([]Zone, error) {
	var out []Zone
	err := a.do(ctx, http.MethodGet, "/zones?per_page=100&account.id="+url.QueryEscape(account), nil, &out)
	return out, err
}

// EnsureTunnel returns the tunnel with that name, creating it (remotely
// configured) when it does not exist.
func (a *API) EnsureTunnel(ctx context.Context, account, name string) (Tunnel, error) {
	var found []Tunnel
	p := "/accounts/" + account + "/cfd_tunnel?is_deleted=false&name=" + url.QueryEscape(name)
	if err := a.do(ctx, http.MethodGet, p, nil, &found); err != nil {
		return Tunnel{}, err
	}
	if len(found) > 0 {
		return found[0], nil
	}
	var t Tunnel
	err := a.do(ctx, http.MethodPost, "/accounts/"+account+"/cfd_tunnel", map[string]any{"name": name, "config_src": "cloudflare"}, &t)
	return t, err
}

func (a *API) TunnelToken(ctx context.Context, account, id string) (string, error) {
	var tok string
	err := a.do(ctx, http.MethodGet, "/accounts/"+account+"/cfd_tunnel/"+id+"/token", nil, &tok)
	return tok, err
}

func (a *API) DeleteTunnel(ctx context.Context, account, id string) error {
	return a.do(ctx, http.MethodDelete, "/accounts/"+account+"/cfd_tunnel/"+id, nil, nil)
}

// PutIngress replaces the routes of the tunnel; a catch-all 404 is appended.
func (a *API) PutIngress(ctx context.Context, account, id string, rules []Ingress) error {
	rules = append(append([]Ingress{}, rules...), Ingress{Service: "http_status:404"})
	return a.do(ctx, http.MethodPut, "/accounts/"+account+"/cfd_tunnel/"+id+"/configurations",
		map[string]any{"config": map[string]any{"ingress": rules}}, nil)
}

// RecordComment marks DNS records created by ducktainer.
const RecordComment = "managed by ducktainer"

// RouteDNS points host at the tunnel with a proxied CNAME. An existing
// record is only replaced when it already points at this tunnel or was
// created by ducktainer.
func (a *API) RouteDNS(ctx context.Context, zone, host, tunnelID string) (string, error) {
	target := tunnelID + ".cfargotunnel.com"
	var existing []DNSRecord
	if err := a.do(ctx, http.MethodGet, "/zones/"+zone+"/dns_records?name="+url.QueryEscape(host), nil, &existing); err != nil {
		return "", err
	}
	rec := DNSRecord{Type: "CNAME", Name: host, Content: target, Proxied: true, Comment: RecordComment}
	var out DNSRecord
	if len(existing) > 0 {
		e := existing[0]
		if len(existing) > 1 || (e.Content != target && !(e.Type == "CNAME" && e.Comment == RecordComment)) {
			return "", i18n.Errorf("%s already has a DNS record (%s %s): remove it or pick another name", host, e.Type, e.Content)
		}
		err := a.do(ctx, http.MethodPut, "/zones/"+zone+"/dns_records/"+e.ID, rec, &out)
		return out.ID, err
	}
	err := a.do(ctx, http.MethodPost, "/zones/"+zone+"/dns_records", rec, &out)
	return out.ID, err
}

func (a *API) DeleteDNS(ctx context.Context, zone, id string) error {
	return a.do(ctx, http.MethodDelete, "/zones/"+zone+"/dns_records/"+id, nil, nil)
}
