//go:build !windows

package main

func runService([]string) bool { return false }
