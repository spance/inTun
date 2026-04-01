package platform

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type KnownHosts struct {
	path     string
	callback ssh.HostKeyCallback
	mu       sync.RWMutex
}

func NewKnownHosts() (*KnownHosts, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot get home dir: %w", err)
	}
	path := filepath.Join(home, ".ssh", "known_hosts")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, fmt.Errorf("cannot create .ssh dir: %w", err)
		}
		if err := os.WriteFile(path, []byte{}, 0600); err != nil {
			return nil, fmt.Errorf("cannot create known_hosts: %w", err)
		}
		return &KnownHosts{path: path, callback: nil}, nil
	}

	callback, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("cannot parse known_hosts: %w", err)
	}

	return &KnownHosts{path: path, callback: callback}, nil
}

func (k *KnownHosts) GetHostKeyCallback(ctx *AuthContext, id int) ssh.HostKeyCallback {
	if k.callback == nil {
		return k.promptCallback(ctx, id)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		k.mu.RLock()
		defer k.mu.RUnlock()

		err := k.callback(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) == 0 {
				if ctx == nil || ctx.RequestChan == nil {
					return errors.New("HOST_KEY_UNKNOWN: no auth context")
				}
				return k.handleUnknownHost(ctx, id, hostname, key)
			}
		}

		return fmt.Errorf("HOST_KEY_MISMATCH: %s key changed", hostname)
	}
}

func (k *KnownHosts) VerifyHostKey(ctx *AuthContext, id int, host string, port string, key ssh.PublicKey) error {
	if port == "" {
		port = "22"
	}
	hostWithPort := host + ":" + port
	if k.callback != nil {
		k.mu.RLock()
		dummyAddr := &net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 22}
		err := k.callback(hostWithPort, dummyAddr, key)
		k.mu.RUnlock()

		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) == 0 {
				if ctx == nil || ctx.RequestChan == nil {
					return errors.New("HOST_KEY_UNKNOWN: no auth context")
				}
				return k.handleUnknownHost(ctx, id, host, key)
			}
		}

		return fmt.Errorf("HOST_KEY_MISMATCH: %s key changed", host)
	}

	if ctx == nil || ctx.RequestChan == nil {
		return errors.New("HOST_KEY_UNKNOWN: no auth context")
	}
	return k.handleUnknownHost(ctx, id, host, key)
}

func (k *KnownHosts) handleUnknownHost(ctx *AuthContext, id int, hostname string, key ssh.PublicKey) error {
	fingerprint := ssh.FingerprintSHA256(key)

	if hostname == "" {
		hostname = "unknown"
	}

	req := AuthRequest{
		ID:          id,
		Type:        AuthRequestHostKey,
		Host:        hostname,
		Fingerprint: fingerprint,
		Response:    make(chan AuthResponse, 1),
	}

	timeout := ctx.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	select {
	case ctx.RequestChan <- req:
	case <-ctx.Cancel.Done():
		return errors.New("cancelled")
	case <-time.After(timeout):
		return errors.New("auth timeout")
	}

	select {
	case resp := <-req.Response:
		if resp.Accept {
			if err := k.Add(hostname, key); err != nil {
				return fmt.Errorf("failed to save host key: %w", err)
			}
			return nil
		}
		return errors.New("host key rejected")
	case <-ctx.Cancel.Done():
		return errors.New("cancelled")
	case <-time.After(timeout):
		return errors.New("auth timeout")
	}
}

func (k *KnownHosts) Add(hostname string, key ssh.PublicKey) error {
	if hostname == "" || hostname == "unknown" {
		return fmt.Errorf("invalid hostname for known_hosts")
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	line := knownhosts.Line([]string{hostname}, key)
	f, err := os.OpenFile(k.path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(line + "\n")
	if err != nil {
		return err
	}

	k.callback, err = knownhosts.New(k.path)
	return err
}

func (k *KnownHosts) promptCallback(ctx *AuthContext, id int) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if ctx == nil || ctx.RequestChan == nil {
			return errors.New("HOST_KEY_UNKNOWN: no auth context")
		}
		return k.handleUnknownHost(ctx, id, hostname, key)
	}
}
