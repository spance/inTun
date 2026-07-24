package sftp

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
)

var (
	ErrOverwriteConfirmationRequired  = errors.New("destination now exists and overwrite was not confirmed")
	ErrAtomicRemoteReplaceUnsupported = errors.New("remote server does not support atomic file replacement")
)

type TransferOptions struct {
	AllowOverwrite     bool
	ApprovedOverwrites map[string]struct{}
}

func (o TransferOptions) allowsOverwrite(path string) bool {
	if o.AllowOverwrite {
		return true
	}
	_, ok := o.ApprovedOverwrites[path]
	return ok
}

func createLocalTransferFile(target string, allowOverwrite bool) (*os.File, string, error) {
	if err := ensureLocalOverwriteAllowed(target, allowOverwrite); err != nil {
		return nil, "", err
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, "", err
	}
	file, err := os.CreateTemp(dir, "."+filepath.Base(target)+".intun-*")
	if err != nil {
		return nil, "", err
	}
	mode := os.FileMode(0644)
	if info, statErr := os.Lstat(target); statErr == nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, "", err
	}
	return file, file.Name(), nil
}

func commitLocalTransfer(file *os.File, tempPath, target string, allowOverwrite bool) error {
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ensureLocalOverwriteAllowed(target, allowOverwrite); err != nil {
		return err
	}
	if !allowOverwrite {
		return installLocalFile(tempPath, target)
	}
	return replaceLocalFile(tempPath, target)
}

func ensureLocalOverwriteAllowed(target string, allowOverwrite bool) error {
	_, err := os.Lstat(target)
	switch {
	case err == nil && !allowOverwrite:
		return ErrOverwriteConfirmationRequired
	case err == nil:
		return nil
	case os.IsNotExist(err):
		return nil
	default:
		return fmt.Errorf("inspect local destination: %w", err)
	}
}

func createRemoteTransferFile(client remoteFileSystem, target string, allowOverwrite bool) (io.WriteCloser, string, error) {
	if err := ensureRemoteOverwriteAllowed(client, target, allowOverwrite); err != nil {
		return nil, "", err
	}
	suffix, err := randomTransferSuffix()
	if err != nil {
		return nil, "", err
	}
	tempPath := JoinRemotePath(RemoteDir(target), "."+pathpkg.Base(target)+".intun-"+suffix)
	writer, err := client.Create(tempPath)
	if err != nil {
		return nil, "", err
	}
	return writer, tempPath, nil
}

func commitRemoteTransfer(client remoteFileSystem, tempPath, target string, allowOverwrite bool) error {
	exists, err := remotePathExists(client, target)
	if err != nil {
		return err
	}
	if exists {
		if !allowOverwrite {
			return ErrOverwriteConfirmationRequired
		}
		if _, ok := client.HasExtension("posix-rename@openssh.com"); !ok {
			return ErrAtomicRemoteReplaceUnsupported
		}
		return client.PosixRename(tempPath, target)
	}
	return client.Rename(tempPath, target)
}

func ensureRemoteOverwriteAllowed(client remoteFileSystem, target string, allowOverwrite bool) error {
	exists, err := remotePathExists(client, target)
	if err != nil {
		return err
	}
	if exists && !allowOverwrite {
		return ErrOverwriteConfirmationRequired
	}
	return nil
}

func remotePathExists(client remoteFileSystem, target string) (bool, error) {
	_, err := client.Lstat(target)
	switch {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, fmt.Errorf("inspect remote destination: %w", err)
	}
}

func randomTransferSuffix() (string, error) {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
