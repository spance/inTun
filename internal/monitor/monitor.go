package monitor

import (
	"context"
	"sync"
	"time"

	"github.com/spance/intun/internal/tunnel"
)

const pingIntervalMultiplier = 5
const defaultMonitorInterval = time.Second

type Monitor struct {
	manager   *tunnel.Manager
	interval  time.Duration
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	startOnce sync.Once
	tick      int
}

func NewMonitor(manager *tunnel.Manager, interval time.Duration) *Monitor {
	if interval <= 0 {
		interval = defaultMonitorInterval
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Monitor{
		manager:  manager,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (m *Monitor) Start() {
	m.startOnce.Do(func() {
		m.wg.Add(1)
		go m.run()
	})
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
	m.manager.Refresh(shouldPing, m.interval)
}
