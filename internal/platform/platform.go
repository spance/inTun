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
	if s.Type != Dynamic && s.RemoteAddr == "" {
		return fmt.Errorf("remote address is required")
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
	RequestChan chan<- AuthRequest
	Cancel      context.Context
	Timeout     time.Duration
}

type SSHConfig struct {
	Host         string
	Port         string
	User         string
	IdentityFile string
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
