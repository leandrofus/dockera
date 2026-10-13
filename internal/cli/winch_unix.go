//go:build !windows

package cli

import (
	"os"
	"os/signal"
	"syscall"
)

// onResize calls fn whenever the terminal window changes size.
func onResize(fn func()) (stop func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	go func() {
		for range sig {
			fn()
		}
	}()
	return func() { signal.Stop(sig) }
}
