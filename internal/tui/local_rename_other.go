//go:build !darwin && !linux && !windows

package tui

import (
	"fmt"
	"os"
)

func renameLocalNoReplace(source, target string) error {
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, target)
}
