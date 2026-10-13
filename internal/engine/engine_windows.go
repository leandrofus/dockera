//go:build windows

package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"ducktainer/internal/config"
	"ducktainer/internal/docker"
	"ducktainer/internal/i18n"
)

// Install sets up Docker as described in the package documentation.
func Install(ctx context.Context, o Options) (Result, error) {
	if o.Out == nil {
		o.Out = io.Discard
	}
	switch o.Mode {
	case "native":
		return installNative(ctx, o)
	case "wsl", "":
		return installWSL(ctx, o)
	}
	return Result{}, i18n.Errorf("unknown mode %q: use wsl (Linux containers) or native (Windows containers)", o.Mode)
}

// ---- helpers ----

func elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

// run executes a command, decodes its output (wsl.exe writes UTF-16) and
// copies it to out; stdin is optional.
func run(ctx context.Context, out io.Writer, stdin string, name string, args ...string) (string, error) {
	cmd := hidden(exec.CommandContext(ctx, name, args...))
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	text := strings.ReplaceAll(DecodeUTF16(buf.Bytes()), "\r\n", "\n")
	if out != nil && strings.TrimSpace(text) != "" {
		fmt.Fprintln(out, strings.TrimRight(text, "\n"))
	}
	if err != nil {
		return text, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return text, nil
}

func powershell(ctx context.Context, script string) (string, error) {
	out, err := run(ctx, nil, "", "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	return strings.TrimSpace(out), err
}

// waitPing waits until the daemon behind url answers.
func waitPing(ctx context.Context, url string, limit time.Duration) error {
	c, err := docker.New(config.Endpoint{Name: "engine", URL: url})
	if err != nil {
		return err
	}
	deadline := time.Now().Add(limit)
	for {
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = c.Ping(pctx)
		cancel()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// ---- native: dockerd.exe as a Windows service ----

func installNative(ctx context.Context, o Options) (Result, error) {
	res := Result{Name: "local", URL: NativePipe}
	if !elevated() {
		return res, i18n.Errorf("run it from an administrator terminal: it enables Windows features and installs a service")
	}

	// Windows containers need the Containers feature, and Hyper-V on
	// desktop editions (they default to Hyper-V isolation).
	features := []string{"Containers"}
	if t, _ := powershell(ctx, "(Get-CimInstance Win32_OperatingSystem).ProductType"); t == "1" {
		features = append(features, "Microsoft-Hyper-V")
	}
	for _, f := range features {
		state, err := powershell(ctx, fmt.Sprintf("(Get-WindowsOptionalFeature -Online -FeatureName %s).State", f))
		if err != nil || state == "" {
			return res, i18n.Errorf("this edition of Windows has no %s feature (Windows Home cannot run Windows containers: use --mode wsl)", f)
		}
		if state == "Enabled" {
			continue
		}
		i18n.Fprintf(o.Out, "Enabling the Windows feature %s…\n", f)
		restart, err := powershell(ctx, fmt.Sprintf("(Enable-WindowsOptionalFeature -Online -FeatureName %s -All -NoRestart).RestartNeeded", f))
		if err != nil {
			return res, i18n.Errorf("cannot enable %s: %v", f, err)
		}
		if strings.EqualFold(restart, "True") {
			res.Reboot = true
		}
	}
	if res.Reboot {
		return res, nil
	}

	m, err := mgr.Connect()
	if err != nil {
		return res, err
	}
	defer m.Disconnect()
	if s, err := m.OpenService("docker"); err == nil {
		defer s.Close()
		i18n.Fprintf(o.Out, "A Docker service is already installed: using it.\n")
		if st, err := s.Query(); err == nil && st.State != svc.Running {
			if err := s.Start(); err != nil {
				return res, i18n.Errorf("cannot start the docker service: %v", err)
			}
		}
		return res, waitPing(ctx, NativePipe, 2*time.Minute)
	}

	version := o.Version
	base := staticIndex + staticArch() + "/"
	if version == "" {
		listing, err := fetch(ctx, base)
		if err != nil {
			return res, err
		}
		if version, err = LatestStatic(string(listing)); err != nil {
			return res, err
		}
	}
	work := filepath.Join(os.TempDir(), "ducktainer-engine")
	zipPath := filepath.Join(work, "docker-"+version+".zip")
	if err := download(ctx, o.Out, base+"docker-"+version+".zip", zipPath, o.SHA256); err != nil {
		return res, err
	}
	dir := filepath.Join(os.Getenv("ProgramFiles"), "Docker")
	if _, err := ExtractDocker(zipPath, dir); err != nil {
		return res, err
	}
	i18n.Fprintf(o.Out, "Docker %s installed in %s\n", version, dir)

	if url, sum, err := latestCompose(ctx); err != nil {
		i18n.Fprintf(o.Out, "warning: compose plugin not installed: %v\n", err)
	} else if err := download(ctx, o.Out, url, filepath.Join(os.Getenv("ProgramData"), "Docker", "cli-plugins", "docker-compose.exe"), sum); err != nil {
		return res, err
	}

	// docker.exe on the PATH of new terminals
	if _, err := powershell(ctx, fmt.Sprintf(`$d = '%s'; $p = [Environment]::GetEnvironmentVariable('Path', 'Machine'); if (-not (($p -split ';') -contains $d)) { [Environment]::SetEnvironmentVariable('Path', ($p.TrimEnd(';') + ';' + $d), 'Machine') }`, dir)); err != nil {
		i18n.Fprintf(o.Out, "warning: could not add %s to PATH: %v\n", dir, err)
	}

	if _, err := run(ctx, o.Out, "", filepath.Join(dir, "dockerd.exe"), "--register-service"); err != nil {
		return res, err
	}
	s, err := m.OpenService("docker")
	if err != nil {
		return res, i18n.Errorf("dockerd did not register its service: %v", err)
	}
	defer s.Close()
	if err := s.Start(); err != nil {
		return res, i18n.Errorf("cannot start the docker service: %v", err)
	}
	return res, waitPing(ctx, NativePipe, 2*time.Minute)
}

// ---- wsl: Docker in a WSL 2 distribution of its own ----

func wsl(ctx context.Context, out io.Writer, args ...string) (string, error) {
	return run(ctx, out, "", "wsl.exe", args...)
}

func installWSL(ctx context.Context, o Options) (Result, error) {
	name := o.Distro
	if name == "" {
		name = DefaultDistro
	}
	res := Result{Name: "wsl", URL: "wsl://" + name}

	if _, err := wsl(ctx, nil, "--version"); err != nil {
		if !elevated() {
			return res, i18n.Errorf("WSL is not installed: run `wsl --install --no-distribution` from an administrator terminal, restart, and run this again")
		}
		i18n.Fprintf(o.Out, "Installing WSL…\n")
		if _, err := wsl(ctx, o.Out, "--install", "--no-distribution"); err != nil {
			return res, err
		}
		res.Reboot = true
		return res, nil
	}

	list, err := wsl(ctx, nil, "-l", "-q")
	if err != nil && !strings.Contains(list, "\n") {
		list = "" // no distributions yet: wsl -l fails
	}
	if !HasDistro(list, name) {
		sums, err := fetch(ctx, ubuntuWSL+"SHA256SUMS")
		if err != nil {
			return res, err
		}
		file, sum, err := PickRootfs(string(sums), ubuntuArch())
		if err != nil {
			return res, err
		}
		root := filepath.Join(os.Getenv("LOCALAPPDATA"), "ducktainer", "wsl")
		image := filepath.Join(root, file)
		if err := download(ctx, o.Out, ubuntuWSL+file, image, sum); err != nil {
			return res, err
		}
		i18n.Fprintf(o.Out, "Creating the WSL distribution %s…\n", name)
		if _, err := wsl(ctx, o.Out, "--import", name, filepath.Join(root, name), image, "--version", "2"); err != nil {
			return res, err
		}
		os.Remove(image)
	}
	if v, _ := wsl(ctx, nil, "-l", "-v"); distroVersion(v, name) == 1 {
		return res, i18n.Errorf("%s runs on WSL 1, which cannot run Docker: `wsl --set-version %s 2` (needs virtualization enabled)", name, name)
	}

	if _, err := run(ctx, o.Out, wslConf, "wsl.exe", "-d", name, "-u", "root", "--", "sh", "-c", "cat > /etc/wsl.conf"); err != nil {
		return res, err
	}
	if _, err := wsl(ctx, nil, "--terminate", name); err != nil {
		return res, err
	}
	i18n.Fprintf(o.Out, "Installing Docker in %s…\n", name)
	if _, err := run(ctx, o.Out, provision, "wsl.exe", "-d", name, "-u", "root", "--", "sh", "-s"); err != nil {
		return res, err
	}

	// WSL stops a distribution (and its containers) when no Windows process
	// uses it: keep one running now and at every logon.
	if !o.NoKeepAlive {
		keep := hidden(exec.Command("wsl.exe", "-d", name, "-u", "root", "--", "sleep", "infinity"))
		if err := keep.Start(); err == nil {
			keep.Process.Release()
		}
		task := "ducktainer WSL " + name
		tr := fmt.Sprintf(`conhost.exe --headless wsl.exe -d %s -u root -- sleep infinity`, name)
		if _, err := run(ctx, nil, "", "schtasks.exe", "/Create", "/F", "/TN", task, "/SC", "ONLOGON", "/TR", tr); err != nil {
			i18n.Fprintf(o.Out, "warning: could not create the logon task that keeps %s running (%v); containers stop when WSL shuts the distribution down\n", name, err)
		}
	}
	return res, waitPing(ctx, res.URL, 3*time.Minute)
}

// Status prints the state of both engines.
func Status(ctx context.Context, out io.Writer, distro string) error {
	if distro == "" {
		distro = DefaultDistro
	}
	native := i18n.Sprintf("not installed")
	if m, err := mgr.Connect(); err == nil {
		if s, err := m.OpenService("docker"); err == nil {
			if st, err := s.Query(); err == nil && st.State == svc.Running {
				native = i18n.Sprintf("service running")
				if err := waitPing(ctx, NativePipe, 0); err != nil {
					native += " · " + err.Error()
				} else {
					native += " · " + NativePipe
				}
			} else {
				native = i18n.Sprintf("service stopped")
			}
			s.Close()
		}
		m.Disconnect()
	}
	i18n.Fprintf(out, "native (Windows containers): %s\n", native)

	state := i18n.Sprintf("not installed")
	if list, err := wsl(ctx, nil, "-l", "-q"); err == nil && HasDistro(list, distro) {
		if err := waitPing(ctx, "wsl://"+distro, 0); err != nil {
			state = i18n.Sprintf("distribution %s present, Docker not answering: %v", distro, err)
		} else {
			state = "wsl://" + distro
		}
	}
	i18n.Fprintf(out, "wsl (Linux containers):      %s\n", state)
	return nil
}
