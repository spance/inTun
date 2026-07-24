package platform

import (
	"context"
	"fmt"
	"time"
)

type TunnelType int

const (
	Local TunnelType = iota
	Remote
	Dynamic
)

func (t TunnelType) String() string {
	switch t {
	case Local:
		return "Local"
	case Remote:
		return "Remote"
	case Dynamic:
		return "Dynamic"
	default:
		return "Unknown"
	}
}

type NetworkProtocol int

const (
	TCP NetworkProtocol = iota
	UDP
)

func (p NetworkProtocol) String() string {
	switch p {
	case TCP:
		return "TCP"
	case UDP:
		return "UDP"
	default:
		return "Unknown"
	}
}

type ForwardSpec struct {
	Type       TunnelType
	Protocol   NetworkProtocol
	LocalAddr  string
	RemoteAddr string
}

func (s ForwardSpec) Validate() error {
	if s.Type < Local || s.Type > Dynamic {
		return fmt.Errorf("unknown tunnel type: %d", s.Type)
	}
	if s.Protocol < TCP || s.Protocol > UDP {
		return fmt.Errorf("unknown network protocol: %d", s.Protocol)
	}
	if s.LocalAddr == "" {
		return fmt.Errorf("local address is required")
	}
	addressOptions := ForwardAddressOptions{AllowHost: true, AllowZeroPort: true}
	if _, err := ParseForwardAddress(s.LocalAddr, addressOptions); err != nil {
		return fmt.Errorf("invalid local address: %w", err)
	}
	if s.Type != Dynamic && s.RemoteAddr == "" {
		return fmt.Errorf("remote address is required")
	}
	if s.Type != Dynamic {
		if _, err := ParseForwardAddress(s.RemoteAddr, addressOptions); err != nil {
			return fmt.Errorf("invalid remote address: %w", err)
		}
	}
	if s.Protocol == UDP && s.Type == Dynamic {
		return fmt.Errorf("%s UDP forwarding is not supported", s.Type)
	}
	return nil
}

type AuthRequestType int

const (
	AuthRequestHostKey AuthRequestType = iota
	AuthRequestPassword
	AuthRequestPassphrase
)

type AuthRequest struct {
	ID          int
	Type        AuthRequestType
	Host        string
	Fingerprint string
	RetryCount  int
	Response    chan AuthResponse
}

type AuthResponse struct {
	Accept   bool
	Password string
}

type AuthContext struct {
	RequestChan    chan<- AuthRequest
	Cancel         context.Context
	CancelRequests func(int)
	TunnelID       int
	Timeout        time.Duration
}

func (c *AuthContext) cancellationContext() context.Context {
	if c != nil && c.Cancel != nil {
		return c.Cancel
	}
	return context.Background()
}

type SSHConfig struct {
	Host           string
	Port           string
	User           string
	IdentityFile   string
	IdentityFiles  []string
	IdentityAgent  string
	IdentitiesOnly bool
	ProxyJumps     []SSHConfig
}

type Connection interface {
	Stop() error
	IsRunning() bool
	Error() string
	GetStats() (uploadBytes, downloadBytes int64)
	Ping() time.Duration
}

type SFTPCapable interface {
	NewSFTPClient() (interface{}, error)
}

type Executor interface {
	Connect(ctx *AuthContext, cfg *SSHConfig, spec ForwardSpec) (Connection, error)
}

func NewExecutor() Executor {
	return newPlatformExecutor()
}
