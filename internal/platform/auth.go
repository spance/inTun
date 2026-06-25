package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func (e *SSHExecutor) getAuthMethods(identityFile string, authCtx *AuthContext, id int, user string, host string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	var errs []string

	if identityFile != "" {
		path := expandPath(identityFile)
		key, err := e.loadPrivateKey(path)
		if err == nil {
			methods = append(methods, ssh.PublicKeys(key))
		} else {
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
		}
	}

	home, err := os.UserHomeDir()
	if err == nil {
		for _, keyFile := range []string{"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"} {
			path := filepath.Join(home, ".ssh", keyFile)
			key, err := e.loadPrivateKey(path)
			if err == nil {
				methods = append(methods, ssh.PublicKeys(key))
			} else {
				errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			}
		}
	}

	if authCtx != nil && authCtx.RequestChan != nil {
		methods = append(methods, ssh.PasswordCallback(func() (string, error) {
			return e.promptPassword(authCtx, id, user, host)
		}))
		methods = append(methods, ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			return e.handleKeyboardInteractive(authCtx, id, user, host, questions, echos)
		}))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no auth methods: %s", strings.Join(errs, "; "))
	}

	return methods, nil
}

func (e *SSHExecutor) promptPassword(authCtx *AuthContext, id int, user string, host string) (string, error) {
	timeout := authCtx.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	for retry := 0; retry < 3; retry++ {
		req := AuthRequest{
			ID:         id,
			Type:       AuthRequestPassword,
			Host:       user + "@" + host,
			RetryCount: retry,
			Response:   make(chan AuthResponse, 1),
		}

		select {
		case authCtx.RequestChan <- req:
		case <-authCtx.Cancel.Done():
			return "", errors.New("cancelled")
		case <-time.After(timeout):
			return "", errors.New("auth timeout")
		}

		select {
		case resp := <-req.Response:
			if !resp.Accept || resp.Password == "" {
				return "", errors.New("password cancelled")
			}
			return resp.Password, nil
		case <-authCtx.Cancel.Done():
			return "", errors.New("cancelled")
		case <-time.After(timeout):
			return "", errors.New("auth timeout")
		}
	}
	return "", errors.New("max password attempts")
}

func (e *SSHExecutor) handleKeyboardInteractive(authCtx *AuthContext, id int, user string, host string, questions []string, echos []bool) ([]string, error) {
	timeout := authCtx.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	answers := make([]string, len(questions))

	for retry := 0; retry < 3; retry++ {
		req := AuthRequest{
			ID:         id,
			Type:       AuthRequestPassword,
			Host:       user + "@" + host,
			RetryCount: retry,
			Response:   make(chan AuthResponse, 1),
		}

		select {
		case authCtx.RequestChan <- req:
		case <-authCtx.Cancel.Done():
			return nil, errors.New("cancelled")
		case <-time.After(timeout):
			return nil, errors.New("auth timeout")
		}

		select {
		case resp := <-req.Response:
			if !resp.Accept || resp.Password == "" {
				return nil, errors.New("password cancelled")
			}
			for i := range questions {
				answers[i] = resp.Password
			}
			return answers, nil
		case <-authCtx.Cancel.Done():
			return nil, errors.New("cancelled")
		case <-time.After(timeout):
			return nil, errors.New("auth timeout")
		}
	}
	return nil, errors.New("max password attempts")
}

func (e *SSHExecutor) loadPrivateKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	key, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, err
	}

	return key, nil
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
