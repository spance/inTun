package sftp

import (
	"context"
	"io"
	"os/exec"
	"strings"
)

func (s *Client) Preview(ctx context.Context, path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := checkCanceled(ctx); err != nil {
		return "", err
	}

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

	content := string(buf[:n])
	if !isBinary(content) {
		return content, nil
	}

	if n > 0 {
		cmd := exec.CommandContext(ctx, "file", "-")
		cmd.Stdin = strings.NewReader(string(buf[:n]))
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			result := strings.TrimSpace(string(out))
			if idx := strings.Index(result, ": "); idx >= 0 {
				result = result[idx+2:]
			}
			return "[binary] " + result, nil
		}
	}

	return "[binary file]", nil
}

func isBinary(s string) bool {
	for _, r := range s {
		if r == 0 {
			return true
		}
	}
	return false
}
