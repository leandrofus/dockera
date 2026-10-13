//go:build !unix

package config

func owner(string) (int, int, bool) { return 0, 0, false }
