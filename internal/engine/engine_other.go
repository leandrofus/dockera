//go:build !windows

package engine

import (
	"context"
	"io"

	"ducktainer/internal/i18n"
)

// Install is only needed on Windows; elsewhere Docker comes from the
// distribution's packages.
func Install(context.Context, Options) (Result, error) {
	return Result{}, i18n.Errorf("`ducktainer engine` installs Docker on Windows; on Linux install Docker from your distribution (docker-ce, docker.io or moby-engine)")
}

// Status reports the engines installed by Install.
func Status(context.Context, io.Writer, string) error {
	return i18n.Errorf("`ducktainer engine` installs Docker on Windows; on Linux install Docker from your distribution (docker-ce, docker.io or moby-engine)")
}

// Relay serves a WSL distribution's Docker daemon to the Windows service.
func Relay(context.Context, string) error {
	return i18n.Errorf("`ducktainer engine relay` only runs on Windows")
}

// Relays lists the running WSL relays (Windows only).
func Relays() []string { return nil }
