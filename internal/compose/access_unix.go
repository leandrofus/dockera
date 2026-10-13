//go:build unix

package compose

import "golang.org/x/sys/unix"

// Readable / Writable check access for the service's own (real) user, the
// one that will actually open the files.
func Readable(path string) bool { return unix.Access(path, unix.R_OK) == nil }
func Writable(path string) bool { return unix.Access(path, unix.W_OK) == nil }
