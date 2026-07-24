package tunnel

import (
	"context"
	"sync"
	"time"

	"github.com/spance/intun/internal/platform"
)

type TunnelType = platform.TunnelType
type NetworkProtocol = platform.NetworkProtocol

const (
	Local   TunnelType      = platform.Local
	Remote  TunnelType      = platform.Remote
	Dynamic TunnelType      = platform.Dynamic
	TCP     NetworkProtocol = platform.TCP
	UDP     NetworkProtocol = platform.UDP
)

type Status int

const (
	StatusStopped Status = iota
	StatusRunning
	StatusConnecting
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusStopped:
		return "Stopped"
	case StatusRunning:
		return "Running"
	case StatusConnecting:
		return "Connecting"
	case StatusError:
		return "Error"
	default:
		return "Unknown"
	}
}

type Snapshot struct {
	ID            int
	Name          string
	SSHConfig     platform.SSHConfig
	HasSSHConfig  bool
	Forward       platform.ForwardSpec
	Status        Status
	Error         string
	Failure       *platform.Failure
	CreatedAt     time.Time
	UploadBytes   int64
	DownloadBytes int64
	UploadSpeed   int64
	DownloadSpeed int64
	Latency       time.Duration
	LatencyKnown  bool
}

type Tunnel struct {
	id            int
	name          string
	sshConfig     platform.SSHConfig
	hasSSHConfig  bool
	forward       platform.ForwardSpec
	status        Status
	conn          platform.Connection
	failure       *platform.Failure
	createdAt     time.Time
	uploadBytes   int64
	downloadBytes int64
	uploadSpeed   int64
	downloadSpeed int64
	latency       time.Duration
	latencyKnown  bool

	generation       uint64
	cancel           context.CancelFunc
	uploadBase       int64
	downloadBase     int64
	lastConnUpload   int64
	lastConnDownload int64
	deleted          bool
	mu               sync.RWMutex
}

func (t *Tunnel) ID() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.id
}

func (t *Tunnel) SSHConfig() *platform.SSHConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.hasSSHConfig {
		return &platform.SSHConfig{}
	}
	copy := t.sshConfig
	return &copy
}

func (t *Tunnel) ForwardSpec() platform.ForwardSpec {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.forward
}

func (t *Tunnel) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var failure *platform.Failure
	if t.failure != nil {
		copy := *t.failure
		failure = &copy
	}
	return Snapshot{
		ID:            t.id,
		Name:          t.name,
		SSHConfig:     t.sshConfig,
		HasSSHConfig:  t.hasSSHConfig,
		Forward:       t.forward,
		Status:        t.status,
		Error:         failure.Error(),
		Failure:       failure,
		CreatedAt:     t.createdAt,
		UploadBytes:   t.uploadBytes,
		DownloadBytes: t.downloadBytes,
		UploadSpeed:   t.uploadSpeed,
		DownloadSpeed: t.downloadSpeed,
		Latency:       t.latency,
		LatencyKnown:  t.latencyKnown,
	}
}

func (t *Tunnel) GetStatus() Status {
	return t.Snapshot().Status
}

func (t *Tunnel) GetError() string {
	return t.Snapshot().Error
}

type StatsSnapshot struct {
	UploadBytes   int64
	DownloadBytes int64
}

func (t *Tunnel) GetSnapshot() StatsSnapshot {
	snapshot := t.Snapshot()
	return StatsSnapshot{UploadBytes: snapshot.UploadBytes, DownloadBytes: snapshot.DownloadBytes}
}

func (t *Tunnel) GetConnection() platform.Connection {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.conn
}
