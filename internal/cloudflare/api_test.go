package cloudflare

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAPI answers like api.cloudflare.com with the given DNS records.
func fakeAPI(t *testing.T, records []DNSRecord, seen *[]string) *API {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*seen = append(*seen, r.Method+" "+r.URL.Path+" "+string(body))
		if r.Header.Get("Authorization") != "Bearer tok" {
			json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]any{{"code": 10000, "message": "Authentication error"}}})
			return
		}
		var result any = map[string]string{"id": "rec-new"}
		if r.Method == http.MethodGet {
			result = records
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	t.Cleanup(srv.Close)
	return &API{Token: "tok", Base: srv.URL}
}

func TestRouteDNS(t *testing.T) {
	var seen []string
	id, err := fakeAPI(t, nil, &seen).RouteDNS(context.Background(), "z", "app.example.com", "tun")
	if err != nil || id != "rec-new" || !strings.HasPrefix(seen[1], "POST /zones/z/dns_records") || !strings.Contains(seen[1], `"content":"tun.cfargotunnel.com"`) {
		t.Fatalf("new record: %v %q %v", err, id, seen)
	}
	// someone else's record is left alone
	seen = nil
	_, err = fakeAPI(t, []DNSRecord{{ID: "r1", Type: "A", Name: "app.example.com", Content: "203.0.113.1"}}, &seen).RouteDNS(context.Background(), "z", "app.example.com", "tun")
	if err == nil || len(seen) != 1 {
		t.Fatalf("foreign record must not be touched: %v %v", err, seen)
	}
	// ours is updated in place
	seen = nil
	_, err = fakeAPI(t, []DNSRecord{{ID: "r1", Type: "CNAME", Name: "app.example.com", Content: "old.cfargotunnel.com", Comment: RecordComment}}, &seen).RouteDNS(context.Background(), "z", "app.example.com", "tun")
	if err != nil || !strings.HasPrefix(seen[1], "PUT /zones/z/dns_records/r1") {
		t.Fatalf("own record: %v %v", err, seen)
	}
}

func TestPutIngressAndErrors(t *testing.T) {
	var seen []string
	api := fakeAPI(t, nil, &seen)
	if err := api.PutIngress(context.Background(), "acc", "tun", []Ingress{{Hostname: "a.example.com", Service: "http://127.0.0.1:1"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen[0], `{"service":"http_status:404"}`) {
		t.Fatalf("missing catch-all: %s", seen[0])
	}
	api.Token = "bad"
	if _, err := api.Accounts(context.Background()); err == nil || !strings.Contains(err.Error(), "Authentication error") {
		t.Fatalf("got %v", err)
	}
}
