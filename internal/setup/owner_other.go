//go:build !unix

package setup

import "io/fs"

func owner(fs.FileInfo) (int, int, bool) { return 0, 0, false }
