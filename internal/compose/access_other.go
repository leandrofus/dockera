//go:build !unix

package compose

import "os"

func Readable(path string) bool { _, err := os.Stat(path); return err == nil }
func Writable(path string) bool { return Readable(path) }
