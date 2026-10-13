package agent

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// InstallService configures and enables a systemd unit for dockler-agent on Linux.
func InstallService(cfg Config) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd service installation is only supported on Linux")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("root privileges required to install systemd service (use sudo)")
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}

	destBin := "/usr/local/bin/dockler-agent"
	if self != destBin {
		data, err := os.ReadFile(self)
		if err != nil {
			return err
		}
		if err := os.WriteFile(destBin+".new", data, 0o755); err != nil {
			return err
		}
		if err := os.Rename(destBin+".new", destBin); err != nil {
			return err
		}
	}

	var args []string
	args = append(args, "agent", "--server", cfg.Server, "--token", cfg.Token, "--name", cfg.Name)
	if cfg.Socket != "" {
		args = append(args, "--socket", cfg.Socket)
	}
	if cfg.Insecure {
		args = append(args, "--insecure")
	}
	if cfg.Fingerprint != "" {
		args = append(args, "--fingerprint", cfg.Fingerprint)
	}

	execLine := destBin + " " + strings.Join(args, " ")

	unitContent := fmt.Sprintf(`[Unit]
Description=dockler agent for Docker
Documentation=https://github.com/leandro/dockler
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`, execLine)

	unitPath := "/etc/systemd/system/dockler-agent.service"
	if err := os.WriteFile(unitPath, []byte(unitContent), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", unitPath, err)
	}

	_ = exec.Command("systemctl", "daemon-reload").Run()
	if err := exec.Command("systemctl", "enable", "--now", "dockler-agent").Run(); err != nil {
		return fmt.Errorf("failed to enable and start dockler-agent: %w", err)
	}

	fmt.Println("dockler-agent service installed and started successfully! ✓")
	fmt.Printf("Service unit: %s\n", unitPath)
	fmt.Println("Check status with: systemctl status dockler-agent")
	return nil
}
