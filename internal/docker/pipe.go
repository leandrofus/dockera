package docker

import (
	"context"
	"net"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// PipePath turns npipe:////./pipe/docker_engine into \\.\pipe\docker_engine.
func PipePath(u *url.URL) string {
	p := u.Path
	if u.Host != "" { // npipe://./pipe/x
		p = "//" + u.Host + p
	}
	return strings.ReplaceAll(p, "/", `\`)
}

// wslDialer reaches the Docker daemon inside a WSL distribution through
// `docker system dial-stdio`, the same way ssh endpoints work.
func wslDialer(distro string) func(ctx context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		cmd := exec.Command("wsl.exe", "-d", distro, "-u", "root", "--", "docker", "system", "dial-stdio")
		hideWindow(cmd)
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
			return nil, err
		}
		c := &cmdConn{cmd: cmd, w: stdin, r: stdout, stderr: stderr, host: distro, kind: "wsl"}
		go func() { c.waitErr = cmd.Wait(); close(c.done()) }()
		return c, nil
	}
}

const pipeTimeout = 10 * time.Second
