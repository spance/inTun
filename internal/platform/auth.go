package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const maxAuthAttempts = 3

func (e *SSHExecutor) getAuthMethods(cfg *SSHConfig, authCtx *AuthContext, id int) ([]ssh.AuthMethod, []io.Closer, error) {
	if cfg == nil {
		return nil, nil, errors.New("no SSH configuration")
	}
	var methods []ssh.AuthMethod
	var closers []io.Closer
	var authErrors []string

	for _, identityFile := range identityFilesForConfig(cfg) {
		path := expandSSHPath(identityFile, cfg)
		key, err := e.loadPrivateKeyWithPrompt(path, authCtx, id, cfg.User, cfg.Host)
		if err == nil {
			methods = append(methods, ssh.PublicKeys(key))
		} else {
			authErrors = append(authErrors, fmt.Sprintf("%s: %v", path, err))
		}
	}

	if !cfg.IdentitiesOnly {
		if method, closer, err := agentAuthMethod(expandSSHPath(cfg.IdentityAgent, cfg)); err == nil && method != nil {
			methods = append(methods, method)
			closers = append(closers, closer)
		} else if err != nil {
			authErrors = append(authErrors, "ssh-agent: "+err.Error())
		}
	}

	if authCtx != nil && authCtx.RequestChan != nil {
		passwordAttempt := 0
		password := ssh.PasswordCallback(func() (string, error) {
			attempt := passwordAttempt
			passwordAttempt++
			return e.promptSecret(authCtx, id, AuthRequestPassword, cfg.User+"@"+cfg.Host, attempt)
		})
		methods = append(methods, ssh.RetryableAuthMethod(password, maxAuthAttempts))

		interactiveAttempt := 0
		interactive := ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			attempt := interactiveAttempt
			interactiveAttempt++
			return e.handleKeyboardInteractive(authCtx, id, cfg.User, cfg.Host, questions, echos, attempt)
		})
		methods = append(methods, ssh.RetryableAuthMethod(interactive, maxAuthAttempts))
	}

	if len(methods) == 0 {
		return nil, closers, fmt.Errorf("no auth methods: %s", strings.Join(authErrors, "; "))
	}
	return methods, closers, nil
}

func identityFilesForConfig(cfg *SSHConfig) []string {
	var files []string
	seen := make(map[string]struct{})
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, "none") {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		files = append(files, value)
	}
	for _, value := range cfg.IdentityFiles {
		add(value)
	}
	add(cfg.IdentityFile)
	if cfg.IdentitiesOnly {
		return files
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return files
	}
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa", "id_dsa"} {
		add(filepath.Join(home, ".ssh", name))
	}
	return files
}

func agentAuthMethod(identityAgent string) (ssh.AuthMethod, io.Closer, error) {
	socket := strings.TrimSpace(identityAgent)
	switch {
	case strings.EqualFold(socket, "none"):
		return nil, nil, nil
	case socket == "", socket == "SSH_AUTH_SOCK":
		socket = os.Getenv("SSH_AUTH_SOCK")
	default:
		socket = expandPathAndEnv(socket)
	}
	if socket == "" {
		return nil, nil, nil
	}
	conn, err := dialAgentSocket(socket, 2*time.Second)
	if err != nil {
		return nil, nil, err
	}
	agentClient := agent.NewClient(conn)
	return ssh.PublicKeysCallback(agentClient.Signers), conn, nil
}

func (e *SSHExecutor) loadPrivateKeyWithPrompt(path string, authCtx *AuthContext, id int, user, host string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := ssh.ParsePrivateKey(keyBytes)
	if err == nil {
		return key, nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) || authCtx == nil || authCtx.RequestChan == nil {
		return nil, err
	}
	label := user + "@" + host + "  " + filepath.Base(path)
	for attempt := 0; attempt < maxAuthAttempts; attempt++ {
		passphrase, promptErr := e.promptSecret(authCtx, id, AuthRequestPassphrase, label, attempt)
		if promptErr != nil {
			return nil, promptErr
		}
		key, parseErr := ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(passphrase))
		if parseErr == nil {
			return key, nil
		}
		err = parseErr
	}
	return nil, fmt.Errorf("private key passphrase rejected: %w", err)
}

func (e *SSHExecutor) promptSecret(authCtx *AuthContext, id int, requestType AuthRequestType, host string, retry int) (string, error) {
	timeout := authCtx.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	cancelCtx := authCtx.cancellationContext()
	req := AuthRequest{
		ID:         id,
		Type:       requestType,
		Host:       host,
		RetryCount: retry,
		Response:   make(chan AuthResponse, 1),
	}

	select {
	case authCtx.RequestChan <- req:
	case <-cancelCtx.Done():
		return "", errors.New("cancelled")
	case <-time.After(timeout):
		return "", errors.New("auth timeout")
	}

	select {
	case resp := <-req.Response:
		if !resp.Accept || resp.Password == "" {
			return "", errors.New("authentication cancelled")
		}
		return resp.Password, nil
	case <-cancelCtx.Done():
		return "", errors.New("cancelled")
	case <-time.After(timeout):
		return "", errors.New("auth timeout")
	}
}

func (e *SSHExecutor) handleKeyboardInteractive(authCtx *AuthContext, id int, user string, host string, questions []string, echos []bool, retry int) ([]string, error) {
	password, err := e.promptSecret(authCtx, id, AuthRequestPassword, user+"@"+host, retry)
	if err != nil {
		return nil, err
	}
	answers := make([]string, len(questions))
	for i := range answers {
		answers[i] = password
	}
	return answers, nil
}

func expandPathAndEnv(path string) string {
	path = os.ExpandEnv(path)
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func expandSSHPath(path string, cfg *SSHConfig) string {
	if path == "" {
		return ""
	}
	home, _ := os.UserHomeDir()
	localUser := ""
	if current, err := user.Current(); err == nil {
		localUser = current.Username
	}
	host, port, remoteUser := "", "22", ""
	if cfg != nil {
		host = cfg.Host
		remoteUser = cfg.User
		if cfg.Port != "" {
			port = cfg.Port
		}
	}
	const percentSentinel = "\x00intun-percent\x00"
	path = strings.ReplaceAll(path, "%%", percentSentinel)
	path = strings.NewReplacer(
		"%d", home,
		"%h", host,
		"%p", port,
		"%r", remoteUser,
		"%u", localUser,
	).Replace(path)
	path = strings.ReplaceAll(path, percentSentinel, "%")
	return expandPathAndEnv(path)
}
