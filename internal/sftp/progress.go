package sftp

import "sync"

type ProgressInfo struct {
	mu        sync.RWMutex
	Done      int64
	Total     int64
	File      string
	FileIndex int
	FileCount int
	Speed     int64
	Active    bool
}

type ProgressSnapshot struct {
	Done      int64
	Total     int64
	File      string
	FileIndex int
	FileCount int
	Speed     int64
	Active    bool
}

func NewProgressInfo(file string, total int64) *ProgressInfo {
	return &ProgressInfo{
		File:   file,
		Total:  total,
		Active: true,
	}
}

func (p *ProgressInfo) SetDone(done int64) {
	p.mu.Lock()
	p.Done = done
	p.mu.Unlock()
}

func (p *ProgressInfo) SetRecursive(done, total int64, file string) {
	p.mu.Lock()
	p.Done = done
	p.Total = total
	p.File = file
	p.mu.Unlock()
}

func (p *ProgressInfo) SetSpeed(speed int64) {
	p.mu.Lock()
	p.Speed = speed
	p.mu.Unlock()
}

func (p *ProgressInfo) SetActive(active bool) {
	p.mu.Lock()
	p.Active = active
	p.mu.Unlock()
}

func (p *ProgressInfo) Snapshot() ProgressSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ProgressSnapshot{
		Done:      p.Done,
		Total:     p.Total,
		File:      p.File,
		FileIndex: p.FileIndex,
		FileCount: p.FileCount,
		Speed:     p.Speed,
		Active:    p.Active,
	}
}
