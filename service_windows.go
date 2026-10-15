//go:build windows

package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"ducktainer/internal/auth"
	"ducktainer/internal/config"
	"ducktainer/internal/docker"
	"ducktainer/internal/engine"
)

// serviceName is the Windows service the MSI installs (`ducktainer serve`
// as LocalSystem).
const serviceName = "ducktainer"

// runService runs `serve` under the Service Control Manager. It returns
// false when this process wasn't started by it.
func runService(args []string) bool {
	if ok, err := svc.IsWindowsService(); err != nil || !ok {
		return false
	}
	dir := config.Dir()
	os.MkdirAll(dir, 0o700)
	logTo(filepath.Join(dir, "ducktainer.log"))
	if err := config.Restrict(dir); err != nil {
		log.Printf("could not restrict the permissions of %s: %v", dir, err)
	}
	restartOnFailure()
	if err := svc.Run(serviceName, &service{args: args}); err != nil {
		log.Printf("service: %v", err)
		os.Exit(1)
	}
	return true
}

// logTo sends the output of the service (which has no console) to a file,
// started over when it grows past 10 MB.
func logTo(path string) {
	if st, err := os.Stat(path); err == nil && st.Size() > 10<<20 {
		os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	os.Stdout, os.Stderr = f, f
	log.SetOutput(f)
}

// restartOnFailure sets the service's recovery options: restart after a
// crash or after `serve` fails (e.g. the port was taken), 5 s later.
func restartOnFailure() {
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return
	}
	defer s.Close()
	restart := mgr.RecoveryAction{Type: mgr.ServiceRestart, Delay: 5 * time.Second}
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{restart, restart, restart}, 24*60*60); err != nil {
		log.Printf("could not set the recovery options: %v", err)
		return
	}
	s.SetRecoveryActionsOnNonCrashFailures(true)
}

type service struct{ args []string }

func (s *service) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- cmdServe(s.args, false) }()
	go watchRelays(ctx)
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-errc:
			log.Printf("serve: %v", err)
			return true, 1 // the service's recovery options restart it
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				return false, 0
			}
		}
	}
}

// watchRelays adds an endpoint for every WSL distribution whose relay an
// administrator is running (the service, as LocalSystem, can't use WSL by
// itself; see engine.Relay), and only trusts relays of administrators.
// Each distribution is added once per run, so removing its endpoint sticks
// until the service restarts.
func watchRelays(ctx context.Context) {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("wsl relays: %v", err)
		return
	}
	cfg.Watch(func() {})
	docker.TrustRelay = func(account, _ string) bool {
		a := cfg.Snapshot().Auth
		role, err := (auth.Policy{AdminGroups: a.AdminGroups, ViewerGroups: a.ViewerGroups, AdminUsers: a.AdminUsers, ViewerUsers: a.ViewerUsers, AllowRoot: a.AllowRoot}).Role(account)
		return err == nil && role == auth.RoleAdmin
	}
	done := map[string]bool{}
	for {
		for _, distro := range engine.Relays() {
			if !done[distro] {
				done[distro] = addRelay(ctx, cfg, distro)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

// addRelay reports whether the distribution needs no more attention.
func addRelay(ctx context.Context, cfg *config.Config, distro string) bool {
	url := "wsl://" + distro
	snap := cfg.Snapshot()
	for _, e := range snap.Endpoints {
		if e.URL == url {
			return true
		}
	}
	ep := config.Endpoint{Name: "wsl", URL: url}
	if _, taken := snap.Endpoint(ep.Name); taken {
		ep.Name = "wsl-" + distro
	}
	c, err := docker.New(ep)
	if err != nil {
		return true
	}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := c.Ping(pctx); err != nil {
		log.Printf("wsl relay %s: %v", distro, err)
		return false
	}
	err = cfg.Update(func(c *config.Config) error {
		for _, e := range c.Endpoints {
			if e.URL == url || e.Name == ep.Name {
				return nil
			}
		}
		c.Endpoints = append(c.Endpoints, ep)
		return nil
	})
	if err != nil {
		return false
	}
	log.Printf("added the endpoint %s (%s), served by its relay", ep.Name, url)
	return true
}
