package compose

import (
	"testing"

	"ducktainer/internal/config"
)

func TestAuthsOnlyForTheEndpoint(t *testing.T) {
	r := Runner{
		Endpoint: config.Endpoint{Name: "prod"},
		Registries: []config.Registry{
			{Name: "all", Server: "ghcr.io", Username: "a", Password: "x"},
			{Name: "prod", Server: "prod.example.com", Username: "b", Password: "y", Endpoints: []string{"prod"}},
			{Name: "dev", Server: "dev.example.com", Username: "c", Password: "z", Endpoints: []string{"dev"}},
		},
	}
	got := r.auths()
	for _, want := range []string{"ghcr.io", "prod.example.com"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing credentials for %s", want)
		}
	}
	if _, ok := got["dev.example.com"]; ok {
		t.Error("credentials limited to another host were handed to prod")
	}
}
