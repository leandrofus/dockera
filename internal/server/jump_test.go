package server

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		image  string
		port   int
		proto  string
		labels map[string]string
		env    map[string]string
		admin  bool
		kind   string
		app    string
		conn   string
		masked bool
	}{
		{image: "postgres:16", port: 5432, proto: "tcp", env: map[string]string{"POSTGRES_USER": "app", "POSTGRES_PASSWORD": "s3cr/t", "POSTGRES_DB": "shop"}, admin: true,
			kind: "tcp", app: "PostgreSQL", conn: "postgresql://app:s3cr%2Ft@{host}:{port}/shop"},
		{image: "postgres:16", port: 5432, proto: "tcp", env: map[string]string{"POSTGRES_PASSWORD": "x"}, admin: false,
			kind: "tcp", app: "PostgreSQL", conn: "postgresql://postgres:••••@{host}:{port}/postgres", masked: true},
		{image: "docker.io/library/mysql:8", port: 3306, proto: "tcp", env: map[string]string{}, admin: true,
			kind: "tcp", app: "MySQL", conn: "mysql://root:<password>@{host}:{port}/"},
		{image: "redis:7-alpine", port: 6379, proto: "tcp", admin: true, kind: "tcp", app: "Redis", conn: "redis://{host}:{port}"},
		{image: "rabbitmq:3-management", port: 15672, proto: "tcp", kind: "http", app: "RabbitMQ"},
		{image: "mongo-express", port: 8081, proto: "tcp", kind: "http"},
		{image: "ghcr.io/acme/api:1", port: 8443, proto: "tcp", kind: "http"},
		{image: "ghcr.io/acme/api:1", port: 7000, proto: "tcp", kind: "tcp"},
		{image: "ghcr.io/acme/api:1", port: 7000, proto: "tcp", labels: map[string]string{"ducktainer.kind.7000": "http"}, kind: "http"},
		{image: "coredns/coredns", port: 53, proto: "udp", kind: "tcp"},
		{image: "my/db:1", port: 5432, proto: "tcp", admin: true, kind: "tcp", app: "PostgreSQL", conn: "postgresql://postgres:<password>@{host}:{port}/postgres"},
	}
	for _, c := range cases {
		e := jumpEntry{Image: c.image, Port: c.port, Proto: c.proto}
		classify(&e, c.labels, c.env, c.admin)
		if e.Kind != c.kind || e.App != c.app || e.Conn != c.conn || e.Masked != c.masked {
			t.Errorf("%s %d/%s: got kind=%s app=%q conn=%q masked=%v", c.image, c.port, c.proto, e.Kind, e.App, e.Conn, e.Masked)
		}
		if e.Kind == "http" && !strings.HasPrefix(e.Scheme, "http") {
			t.Errorf("%s %d: http without scheme", c.image, c.port)
		}
	}
}

func TestImageName(t *testing.T) {
	for in, want := range map[string]string{
		"postgres:16":                       "postgres",
		"ghcr.io/acme/postgres:16@sha256:x": "acme/postgres",
		"localhost:5000/app":                "app",
		"bitnami/redis":                     "bitnami/redis",
	} {
		if got := imageName(in); got != want {
			t.Errorf("imageName(%q) = %q, want %q", in, got, want)
		}
	}
}
