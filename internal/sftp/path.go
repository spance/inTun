package sftp

import (
	"fmt"
	pathpkg "path"
	"path/filepath"
	"strings"
)

func JoinRemotePath(base string, parts ...string) string {
	path := strings.TrimRight(base, "/")
	for _, part := range parts {
		part = filepath.ToSlash(part)
		part = strings.ReplaceAll(part, "\\", "/")
		part = strings.Trim(part, "/")
		if part == "" || part == "." {
			continue
		}
		if path == "" {
			path = "/" + part
		} else {
			path += "/" + part
		}
	}
	if path == "" {
		return "/"
	}
	return path
}

func RemoteDir(remotePath string) string {
	dir := pathpkg.Dir(remotePath)
	if dir == "." {
		return "/"
	}
	return dir
}

func RemoteBase(remotePath string) string {
	return pathpkg.Base(remotePath)
}

func RemoteRel(base, target string) (string, error) {
	base = pathpkg.Clean(base)
	target = pathpkg.Clean(target)
	if base == "." || base == "/" {
		return strings.TrimPrefix(target, "/"), nil
	}
	if target == base {
		return ".", nil
	}
	prefix := strings.TrimRight(base, "/") + "/"
	if !strings.HasPrefix(target, prefix) {
		return "", fmt.Errorf("%q is not under %q", target, base)
	}
	return strings.TrimPrefix(target, prefix), nil
}
