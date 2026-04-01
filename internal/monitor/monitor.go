package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/spance/intun/internal/tunnel"
)

const pingIntervalMultiplier = 5

type Monitor struct {
	manager  *tunnel.Manager
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	tick     int
}

func NewMonitor(manager *tunnel.Manager, interval time.Duration) *Monitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Monitor{
		manager:  manager,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (m *Monitor) Start() {
	m.wg.Add(1)
	go m.run()
}

func (m *Monitor) Stop() {
	m.cancel()
	m.wg.Wait()
}

func (m *Monitor) run() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.tick++
			m.updateAllStats()
		}
	}
}

func (m *Monitor) updateAllStats() {
	shouldPing := m.tick%pingIntervalMultiplier == 0

	for _, t := range m.manager.List() {
		t.CheckStatus()
		if t.GetStatus() != tunnel.StatusRunning {
			continue
		}
		go m.updateTunnelStats(t, shouldPing)
	}
}

func (m *Monitor) updateTunnelStats(t *tunnel.Tunnel, shouldPing bool) {
	var latency time.Duration
	var up, down, speedUp, speedDown int64

	if t.Conn != nil {
		if shouldPing {
			latency = t.Conn.Ping()
		}
		up, down = t.Conn.GetStats()

		prevUp, prevDown := t.UploadBytes, t.DownloadBytes
		deltaUp := up - prevUp
		deltaDown := down - prevDown

		speedUp = int64(float64(deltaUp) * float64(time.Second) / float64(m.interval))
		speedDown = int64(float64(deltaDown) * float64(time.Second) / float64(m.interval))
	}

	t.UpdateStats(up, down, speedUp, speedDown, latency, shouldPing)
}
