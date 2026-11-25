//go:build linux

package server

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func replaceByExchange(dest, src string) error {
	if filepath.Dir(dest) != filepath.Dir(src) {
		return fmt.Errorf("程序不在同一目录")
	}
	return unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dest, unix.RENAME_EXCHANGE)
}
