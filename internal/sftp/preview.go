package sftp

import (
	"context"
	"io"
	"net/http"
	"unicode/utf8"
)

func (s *Client) Preview(ctx context.Context, path string) (string, error) {
	_, done, err := s.beginOperation(ctx)
	if err != nil {
		return "", err
	}
	defer done()

	r, err := s.client.Open(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	stopWatching := closeOnCancel(ctx, r)
	defer stopWatching()

	buf := make([]byte, 4096)
	if err := checkCanceled(ctx); err != nil {
		return "", err
	}
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}

	content := buf[:n]
	if !isBinaryBytes(content) {
		return string(content), nil
	}

	if n > 0 {
		return "[binary] " + http.DetectContentType(content), nil
	}

	return "[binary file]", nil
}

func isBinaryBytes(data []byte) bool {
	if !utf8.Valid(data) {
		return true
	}
	for _, value := range data {
		if value == 0 {
			return true
		}
	}
	return false
}
