//go:build !windows

package config

import "os"

func machineDir() string { return "" }

func replaceFile(tmp, path string) error { return os.Rename(tmp, path) }
