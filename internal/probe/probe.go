// Package probe tells whether a TCP port speaks HTTP or HTTPS.
package probe

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"time"
)

// Dialer opens a raw connection to the port being probed.
type Dialer func(ctx context.Context) (net.Conn, error)

// Sniff returns "http", "https" or "" (not HTTP, or unreachable).
func Sniff(ctx context.Context, dial Dialer) string {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	if conn, err := dial(ctx); err == nil {
		ok := speaksHTTP(ctx, conn)
		conn.Close()
		if ok {
			return "http"
		}
	} else {
		return ""
	}
	conn, err := dial(ctx)
	if err != nil {
		return ""
	}
	tc := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, ServerName: "localhost"})
	defer tc.Close()
	if dl, ok := ctx.Deadline(); ok {
		tc.SetDeadline(dl)
	}
	if tc.HandshakeContext(ctx) != nil {
		return ""
	}
	if speaksHTTP(ctx, tc) {
		return "https"
	}
	return ""
}

func speaksHTTP(ctx context.Context, conn net.Conn) bool {
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	if _, err := conn.Write([]byte("HEAD / HTTP/1.0\r\nHost: localhost\r\n\r\n")); err != nil {
		return false
	}
	buf := make([]byte, 5)
	n, _ := io.ReadFull(conn, buf)
	return string(buf[:n]) == "HTTP/"
}
