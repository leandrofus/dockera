package cli

import (
	"os"
	"time"

	"golang.org/x/term"
)

// onResize polls the console size: Windows has no SIGWINCH.
func onResize(fn func()) (stop func()) {
	done := make(chan struct{})
	go func() {
		w, h, _ := term.GetSize(int(os.Stdin.Fd()))
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if nw, nh, err := term.GetSize(int(os.Stdin.Fd())); err == nil && (nw != w || nh != h) {
					w, h = nw, nh
					fn()
				}
			}
		}
	}()
	return func() { close(done) }
}
