package tunnel

import (
	"context"
	"errors"
	"time"

	"github.com/spance/intun/internal/platform"
)

var errTunnelDeleted = errors.New("tunnel has been deleted")

type detachedRuntime struct {
	cancel       context.CancelFunc
	conn         platform.Connection
	lastUpload   int64
	lastDownload int64
}

func (t *Tunnel) beginStart(cancel context.CancelFunc) (uint64, detachedRuntime, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.deleted {
		return 0, detachedRuntime{}, errTunnelDeleted
	}
	previous := t.detachRuntimeLocked()
	t.generation++
	t.cancel = cancel
	t.status = StatusConnecting
	t.failure = nil
	t.uploadBase = t.uploadBytes
	t.downloadBase = t.downloadBytes
	t.lastConnUpload = 0
	t.lastConnDownload = 0
	t.uploadSpeed = 0
	t.downloadSpeed = 0
	t.latency = 0
	t.latencyKnown = false
	return t.generation, previous, nil
}

func (t *Tunnel) installConnection(generation uint64, conn platform.Connection) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.deleted || t.generation != generation || t.status != StatusConnecting {
		return false
	}
	t.conn = conn
	switch {
	case conn.IsRunning():
		t.status = StatusRunning
	case conn.Error() != "":
		t.status = StatusError
		t.failure = platform.ParseFailure(conn.Error())
	}
	return true
}

func (t *Tunnel) stopRuntime(status Status, failure *platform.Failure) detachedRuntime {
	t.mu.Lock()
	defer t.mu.Unlock()
	previous := t.detachRuntimeLocked()
	t.generation++
	t.status = status
	t.failure = failure
	t.uploadSpeed = 0
	t.downloadSpeed = 0
	t.latency = 0
	t.latencyKnown = false
	return previous
}

func (t *Tunnel) deleteRuntime() detachedRuntime {
	t.mu.Lock()
	defer t.mu.Unlock()
	previous := t.detachRuntimeLocked()
	t.generation++
	t.deleted = true
	t.status = StatusStopped
	t.failure = nil
	t.uploadSpeed = 0
	t.downloadSpeed = 0
	t.latency = 0
	t.latencyKnown = false
	return previous
}

func (t *Tunnel) detachRuntimeLocked() detachedRuntime {
	previous := detachedRuntime{
		cancel:       t.cancel,
		conn:         t.conn,
		lastUpload:   t.lastConnUpload,
		lastDownload: t.lastConnDownload,
	}
	t.cancel = nil
	t.conn = nil
	return previous
}

func (t *Tunnel) captureDetachedTotals(runtime detachedRuntime) {
	if runtime.conn == nil {
		return
	}
	up, down := runtime.conn.GetStats()
	deltaUp := max64(0, up-runtime.lastUpload)
	deltaDown := max64(0, down-runtime.lastDownload)

	t.mu.Lock()
	t.uploadBytes += deltaUp
	t.downloadBytes += deltaDown
	t.uploadBase += deltaUp
	t.downloadBase += deltaDown
	t.mu.Unlock()
}

func (t *Tunnel) setFailure(generation uint64, failure *platform.Failure) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.deleted || generation != 0 && t.generation != generation {
		return false
	}
	t.status = StatusError
	t.failure = failure
	t.uploadSpeed = 0
	t.downloadSpeed = 0
	return true
}

func (t *Tunnel) CheckStatus() {
	t.mu.RLock()
	status := t.status
	conn := t.conn
	generation := t.generation
	t.mu.RUnlock()
	if conn == nil {
		return
	}

	var next Status
	var failure *platform.Failure
	switch status {
	case StatusConnecting:
		if conn.IsRunning() {
			next = StatusRunning
		} else if errMsg := conn.Error(); errMsg != "" {
			next = StatusError
			failure = platform.ParseFailure(errMsg)
		} else {
			return
		}
	case StatusRunning:
		if conn.IsRunning() {
			return
		}
		next = StatusError
		failure = platform.ParseFailure(conn.Error())
		if failure == nil {
			failure = &platform.Failure{Code: platform.FailureSSHConnectionLost, Detail: "connection stopped"}
		}
	default:
		return
	}

	t.mu.Lock()
	if !t.deleted && t.generation == generation && t.conn == conn && t.status == status {
		t.status = next
		t.failure = failure
	}
	t.mu.Unlock()
}

func (t *Tunnel) refreshStats(shouldPing bool, interval time.Duration) {
	t.mu.RLock()
	if t.status != StatusRunning || t.conn == nil {
		t.mu.RUnlock()
		return
	}
	conn := t.conn
	generation := t.generation
	t.mu.RUnlock()

	var latency time.Duration
	if shouldPing {
		latency = conn.Ping()
	}
	up, down := conn.GetStats()

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.deleted || t.generation != generation || t.conn != conn || t.status != StatusRunning {
		return
	}
	if interval <= 0 {
		interval = time.Second
	}
	if up < t.lastConnUpload {
		t.uploadBase = t.uploadBytes
		t.lastConnUpload = 0
	}
	if down < t.lastConnDownload {
		t.downloadBase = t.downloadBytes
		t.lastConnDownload = 0
	}
	deltaUp := up - t.lastConnUpload
	deltaDown := down - t.lastConnDownload
	t.uploadBytes = t.uploadBase + up
	t.downloadBytes = t.downloadBase + down
	t.uploadSpeed = max64(0, int64(float64(deltaUp)*float64(time.Second)/float64(interval)))
	t.downloadSpeed = max64(0, int64(float64(deltaDown)*float64(time.Second)/float64(interval)))
	t.lastConnUpload = up
	t.lastConnDownload = down
	if shouldPing {
		t.latency = latency
		t.latencyKnown = latency > 0
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
