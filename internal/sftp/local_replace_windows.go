//go:build windows

package sftp

import "golang.org/x/sys/windows"

func installLocalFile(source, target string) error {
	return moveLocalFile(source, target, windows.MOVEFILE_WRITE_THROUGH)
}

func replaceLocalFile(source, target string) error {
	return moveLocalFile(source, target, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func moveLocalFile(source, target string, flags uint32) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(sourcePtr, targetPtr, flags)
}
