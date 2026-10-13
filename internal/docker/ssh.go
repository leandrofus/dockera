package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// sshDialer connects through the system ssh client and runs
// `docker system dial-stdio` remotely, exactly like the docker CLI does for
// DOCKER_HOST=ssh://. A ControlMaster socket keeps one SSH session alive so
// each new HTTP connection is cheap.
func sshDialer(u *url.URL, identity string) func(ctx context.Context) (net.Conn, error) {
	ctlDir := filepath.Join(os.TempDir(), fmt.Sprintf("dockler-ssh-%d", os.Getuid()))
	_ = os.MkdirAll(ctlDir, 0o700)
	return func(ctx context.Context) (net.Conn, error) {
		args := SSHArgs(u, identity)
		args = append(args,
			"-o", "ControlMaster=auto",
			"-o", "ControlPath="+filepath.Join(ctlDir, "%C"),
			"-o", "ControlPersist=120s",
			"--", "docker", "system", "dial-stdio")
		// The process must outlive the dial context, it is the connection.
		cmd := exec.Command("ssh", args...)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		stderr := &limitedBuffer{max: 4096}
		cmd.Stderr = stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("ssh: %w", err)
		}
		c := &cmdConn{cmd: cmd, w: stdin, r: stdout, stderr: stderr, host: u.Host}
		go func() { c.waitErr = cmd.Wait(); close(c.done()) }()
		return c, nil
	}
}

// SSHArgs builds the base ssh arguments (destination last) for a ssh:// URL.
func SSHArgs(u *url.URL, identity string) []string {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=30"}
	if p := u.Port(); p != "" {
		args = append(args, "-p", p)
	}
	if identity != "" {
		args = append(args, "-i", identity)
	}
	dest := u.Hostname()
	if u.User != nil && u.User.Username() != "" {
		dest = u.User.Username() + "@" + dest
	}
	return append(args, dest)
}

type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buf.String())
}

type cmdConn struct {
	cmd     *exec.Cmd
	w       io.WriteCloser
	r       io.ReadCloser
	stderr  *limitedBuffer
	host    string
	once    sync.Once
	doneCh  chan struct{}
	waitErr error
	mu      sync.Mutex
}

func (c *cmdConn) done() chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.doneCh == nil {
		c.doneCh = make(chan struct{})
	}
	return c.doneCh
}

func (c *cmdConn) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if err == io.EOF && n == 0 {
		if msg := c.stderr.String(); msg != "" {
			return 0, fmt.Errorf("ssh %s: %s", c.host, msg)
		}
	}
	return n, err
}

func (c *cmdConn) Write(p []byte) (int, error) { return c.w.Write(p) }

func (c *cmdConn) Close() error {
	c.once.Do(func() {
		c.w.Close()
		select {
		case <-c.done():
		case <-time.After(2 * time.Second):
			if c.cmd.Process != nil {
				c.cmd.Process.Kill()
			}
		}
	})
	return nil
}

// CloseWrite lets the HTTP stack half-close like a TCP conn.
func (c *cmdConn) CloseWrite() error { return c.w.Close() }

type dummyAddr string

func (a dummyAddr) Network() string { return "ssh" }
func (a dummyAddr) String() string  { return string(a) }

func (c *cmdConn) LocalAddr() net.Addr                { return dummyAddr("local") }
func (c *cmdConn) RemoteAddr() net.Addr               { return dummyAddr(c.host) }
func (c *cmdConn) SetDeadline(t time.Time) error      { return nil }
func (c *cmdConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *cmdConn) SetWriteDeadline(t time.Time) error { return nil }
