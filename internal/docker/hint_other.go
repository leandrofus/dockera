//go:build !unix

package docker

func permissionHint(_ string, err error) error { return err }
