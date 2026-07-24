package sftp

import (
	"context"
	"fmt"
	"io"
	"os"
)

func (s *Client) Download(ctx context.Context, remotePath, localPath string, progress func(int64)) error {
	return s.DownloadWithOptions(ctx, remotePath, localPath, TransferOptions{}, progress)
}

func (s *Client) DownloadWithOptions(ctx context.Context, remotePath, localPath string, options TransferOptions, progress func(int64)) error {
	_, done, err := s.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer done()

	r, err := s.client.Open(remotePath)
	if err != nil {
		return err
	}
	defer r.Close()
	stopWatchingRemote := closeOnCancel(ctx, r)
	defer stopWatchingRemote()

	w, tempPath, err := createLocalTransferFile(localPath, options.AllowOverwrite)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)
	defer w.Close()
	stopWatchingLocal := closeOnCancel(ctx, w)
	defer stopWatchingLocal()

	if err := copyWithContext(ctx, r, w, progress); err != nil {
		return err
	}
	return commitLocalTransfer(w, tempPath, localPath, options.AllowOverwrite)
}

func (s *Client) Upload(ctx context.Context, localPath, remotePath string, progress func(int64)) error {
	return s.UploadWithOptions(ctx, localPath, remotePath, TransferOptions{}, progress)
}

func (s *Client) UploadWithOptions(ctx context.Context, localPath, remotePath string, options TransferOptions, progress func(int64)) error {
	_, done, err := s.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer done()

	r, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer r.Close()
	stopWatchingLocal := closeOnCancel(ctx, r)
	defer stopWatchingLocal()

	if err := s.client.MkdirAll(RemoteDir(remotePath)); err != nil {
		return err
	}
	w, tempPath, err := createRemoteTransferFile(s.client, remotePath, options.AllowOverwrite)
	if err != nil {
		return err
	}
	defer s.client.Remove(tempPath)
	defer w.Close()
	stopWatchingRemote := closeOnCancel(ctx, w)
	defer stopWatchingRemote()

	if err := copyWithContext(ctx, r, w, progress); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return commitRemoteTransfer(s.client, tempPath, remotePath, options.AllowOverwrite)
}

func (s *Client) copyRemoteToLocal(ctx context.Context, remotePath, localPath string, allowOverwrite bool) error {
	if err := checkCanceled(ctx); err != nil {
		return err
	}
	r, err := s.client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote file: %w", err)
	}
	defer r.Close()
	stopWatchingRemote := closeOnCancel(ctx, r)
	defer stopWatchingRemote()

	w, tempPath, err := createLocalTransferFile(localPath, allowOverwrite)
	if err != nil {
		return fmt.Errorf("create local file: %w", err)
	}
	defer os.Remove(tempPath)
	defer w.Close()
	stopWatchingLocal := closeOnCancel(ctx, w)
	defer stopWatchingLocal()

	if err := copyWithContext(ctx, r, w, nil); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}
	if err := commitLocalTransfer(w, tempPath, localPath, allowOverwrite); err != nil {
		return fmt.Errorf("commit local file: %w", err)
	}
	return nil
}

func (s *Client) copyLocalToRemote(ctx context.Context, localPath, remotePath string, allowOverwrite bool) error {
	if err := checkCanceled(ctx); err != nil {
		return err
	}
	r, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer r.Close()
	stopWatchingLocal := closeOnCancel(ctx, r)
	defer stopWatchingLocal()

	if err := s.client.MkdirAll(RemoteDir(remotePath)); err != nil {
		return fmt.Errorf("create remote directory: %w", err)
	}
	w, tempPath, err := createRemoteTransferFile(s.client, remotePath, allowOverwrite)
	if err != nil {
		return fmt.Errorf("create remote file: %w", err)
	}
	defer s.client.Remove(tempPath)
	defer w.Close()
	stopWatchingRemote := closeOnCancel(ctx, w)
	defer stopWatchingRemote()

	if err := copyWithContext(ctx, r, w, nil); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close remote file: %w", err)
	}
	if err := commitRemoteTransfer(s.client, tempPath, remotePath, allowOverwrite); err != nil {
		return fmt.Errorf("commit remote file: %w", err)
	}
	return nil
}

func copyWithContext(ctx context.Context, r io.Reader, w io.Writer, progress func(int64)) error {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		if err := checkCanceled(ctx); err != nil {
			return err
		}
		n, readErr := r.Read(buf)
		if n > 0 {
			if err := checkCanceled(ctx); err != nil {
				return err
			}
			written, writeErr := w.Write(buf[:n])
			if writeErr != nil {
				if ctxErr := checkCanceled(ctx); ctxErr != nil {
					return ctxErr
				}
				return writeErr
			}
			if written != n {
				if ctxErr := checkCanceled(ctx); ctxErr != nil {
					return ctxErr
				}
				return io.ErrShortWrite
			}
			total += int64(n)
			if progress != nil {
				progress(total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if ctxErr := checkCanceled(ctx); ctxErr != nil {
				return ctxErr
			}
			return readErr
		}
	}
	return checkCanceled(ctx)
}

func closeOnCancel(ctx context.Context, c io.Closer) func() {
	if ctx == nil || c == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-done:
		}
	}()
	return func() {
		close(done)
	}
}
