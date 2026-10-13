package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"ducktainer/internal/i18n"
)

// Besides proxying the Docker socket, an agent stream can carry a raw TCP
// connection to a port on the agent's host:
//
//	GET /_ducktainer/dial?addr=127.0.0.1:5432 HTTP/1.1
//
// answered by "HTTP/1.1 200 OK" and then the bytes of that connection. The
// server uses it to probe and to tunnel ports of nodes behind NAT. Agents
// that predate it forward the request to Docker, which answers 404.
const dialPath = "/_ducktainer/dial"

// ErrDialUnsupported means the agent is too old to dial.
var ErrDialUnsupported = errors.New("agent does not support dialing (update it)")

// DialTarget opens a connection to addr as seen from the agent's host.
func (h *Hub) DialTarget(ctx context.Context, name, addr string) (net.Conn, error) {
	stream, err := h.Dial(ctx, name)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		stream.SetDeadline(dl)
	}
	req := fmt.Sprintf("GET %s?addr=%s HTTP/1.1\r\nHost: agent\r\n\r\n", dialPath, url.QueryEscape(addr))
	if _, err := io.WriteString(stream, req); err != nil {
		stream.Close()
		return nil, err
	}
	br := bufio.NewReader(stream)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		stream.Close()
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		stream.Close()
		return nil, ErrDialUnsupported
	default:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		stream.Close()
		return nil, i18n.Errorf("agent %s cannot reach %s: %s", name, addr, string(msg))
	}
	stream.SetDeadline(time.Time{})
	return &bufConn{Conn: stream, r: br}, nil
}

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// serveDial handles a dial request on the agent side. Only addresses of this
// host (loopback, its interfaces and the subnets behind them, which include
// Docker's bridges) are allowed.
func serveDial(stream net.Conn, br *bufio.Reader) {
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	addr := req.URL.Query().Get("addr")
	if err := allowedTarget(addr); err != nil {
		writeStatus(stream, http.StatusForbidden, err.Error())
		return
	}
	target, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		writeStatus(stream, http.StatusBadGateway, err.Error())
		return
	}
	defer target.Close()
	if _, err := io.WriteString(stream, "HTTP/1.1 200 OK\r\n\r\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(target, br)
		if tc, ok := target.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		io.Copy(stream, target)
		done <- struct{}{}
	}()
	<-done
}

func writeStatus(w io.Writer, code int, msg string) {
	fmt.Fprintf(w, "HTTP/1.1 %d %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", code, http.StatusText(code), len(msg), msg)
}

func allowedTarget(addr string) error {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address %q", addr)
	}
	ip := ap.Addr().Unmap()
	if ip.IsLoopback() {
		return nil
	}
	ifaces, err := net.InterfaceAddrs()
	if err != nil {
		return err
	}
	for _, a := range ifaces {
		if n, ok := a.(*net.IPNet); ok {
			if p, err := netip.ParsePrefix(n.String()); err == nil && p.Contains(ip) {
				return nil
			}
		}
	}
	log.Printf("[agent] refused dial to %s: not a local address", addr)
	return fmt.Errorf("%s is not an address of this host", ip)
}
