//go:build darwin

package tui

import "golang.org/x/sys/unix"

func renameLocalNoReplace(source, target string) error {
	return unix.RenamexNp(source, target, unix.RENAME_EXCL)
}
