package platform

import (
	"context"
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
	WaitForReady(timeout time.Duration) bool
	GetStats() (uploadBytes, downloadBytes int64)
	Ping() time.Duration
}

type Executor interface {
	Connect(ctx *AuthContext, cfg *SSHConfig, tunnelType TunnelType, localPort, remotePort string) (Connection, error)
}

func NewExecutor() Executor {
	return newPlatformExecutor()
}
