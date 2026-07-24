//go:build !windows

package sftp

import "os"

func installLocalFile(source, target string) error {
	if err := os.Link(source, target); err != nil {
		return err
	}
	return os.Remove(source)
}

func replaceLocalFile(source, target string) error {
	return os.Rename(source, target)
}
