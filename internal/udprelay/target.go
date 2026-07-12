package udprelay

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

type TargetRelay struct {
	transport FrameTransport
	target    *net.UDPAddr
	options   RelayOptions
	hooks     TargetHooks
	ctx       context.Context
	mu        sync.Mutex
	sessions  map[uint32]*targetAssociation
	wg        sync.WaitGroup
}

type targetAssociation struct {
	conn     *net.UDPConn
	lastSeen time.Time
}

func NewTargetRelay(transport FrameTransport, target string, options RelayOptions, hooks TargetHooks) (*TargetRelay, error) {
	targetAddr, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return nil, fmt.Errorf("resolve UDP target: %w", err)
	}
	return &TargetRelay{
		transport: transport,
		target:    targetAddr,
		options:   options.withDefaults(),
		hooks:     hooks,
		sessions:  make(map[uint32]*targetAssociation),
	}, nil
}

func (r *TargetRelay) Serve(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	serveCtx, ctxCancel := context.WithCancel(ctx)
	r.ctx = serveCtx
	stopCancellationWatch := watchTransportCancellation(ctx, r.transport)
	defer func() {
		stopCancellationWatch()
		ctxCancel()
		_ = r.transport.Close()
		r.closeAssociations()
	}()
	r.wg.Add(1)
	go r.sweep()

	for {
		frame, err := r.transport.ReadFrame()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if isClosedError(err) {
				return nil
			}
			return fmt.Errorf("read target relay frame: %w", err)
		}
		switch frame.Type {
		case FrameData:
			if frame.AssociationID == 0 {
				return fmt.Errorf("%w: zero association ID", ErrInvalidFrame)
			}
			if err := r.send(frame.AssociationID, frame.Payload); err != nil {
				r.reportAssociationError(frame.AssociationID, err)
				_ = r.transport.WriteFrame(Frame{Type: FrameError, AssociationID: frame.AssociationID, Payload: []byte(err.Error())})
			}
		case FrameClose:
			r.remove(frame.AssociationID, false)
		default:
			return fmt.Errorf("%w: unexpected target frame type %d", ErrInvalidFrame, frame.Type)
		}
	}
}

func (r *TargetRelay) send(id uint32, payload []byte) error {
	association, err := r.getOrCreate(id)
	if err != nil {
		return err
	}
	if _, err := association.conn.Write(payload); err != nil {
		r.removeAssociation(id, association, false)
		return fmt.Errorf("send UDP datagram: %w", err)
	}
	r.mu.Lock()
	if current := r.sessions[id]; current == association {
		current.lastSeen = time.Now()
	}
	r.mu.Unlock()
	return nil
}

func (r *TargetRelay) getOrCreate(id uint32) (*targetAssociation, error) {
	r.mu.Lock()
	if association := r.sessions[id]; association != nil {
		association.lastSeen = time.Now()
		r.mu.Unlock()
		return association, nil
	}
	if len(r.sessions) >= r.options.MaxAssociations {
		r.mu.Unlock()
		return nil, ErrAssociationLimit
	}
	r.mu.Unlock()

	conn, err := net.DialUDP("udp", nil, r.target)
	if err != nil {
		return nil, fmt.Errorf("connect UDP target: %w", err)
	}
	association := &targetAssociation{conn: conn, lastSeen: time.Now()}
	r.mu.Lock()
	if existing := r.sessions[id]; existing != nil {
		r.mu.Unlock()
		_ = conn.Close()
		return existing, nil
	}
	r.sessions[id] = association
	r.mu.Unlock()
	r.wg.Add(1)
	go r.receive(id, association)
	return association, nil
}

func (r *TargetRelay) receive(id uint32, association *targetAssociation) {
	defer r.wg.Done()
	buffer := make([]byte, MaxDatagramSize)
	for {
		n, err := association.conn.Read(buffer)
		if err != nil {
			if r.removeAssociation(id, association, false) {
				r.reportAssociationError(id, err)
				_ = r.transport.WriteFrame(Frame{Type: FrameError, AssociationID: id, Payload: []byte(fmt.Sprintf("receive UDP response: %v", err))})
			}
			return
		}
		r.mu.Lock()
		if r.sessions[id] != association {
			r.mu.Unlock()
			return
		}
		association.lastSeen = time.Now()
		r.mu.Unlock()
		payload := append([]byte(nil), buffer[:n]...)
		if err := r.transport.WriteFrame(Frame{Type: FrameData, AssociationID: id, Payload: payload}); err != nil {
			r.removeAssociation(id, association, false)
			return
		}
	}
}

func (r *TargetRelay) sweep() {
	defer r.wg.Done()
	ticker := time.NewTicker(r.options.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case now := <-ticker.C:
			cutoff := now.Add(-r.options.IdleTimeout)
			var candidates []uint32
			r.mu.Lock()
			for id, association := range r.sessions {
				if !association.lastSeen.After(cutoff) {
					candidates = append(candidates, id)
				}
			}
			r.mu.Unlock()
			for _, id := range candidates {
				r.expire(id, cutoff)
			}
		}
	}
}

func (r *TargetRelay) expire(id uint32, cutoff time.Time) {
	r.mu.Lock()
	association := r.sessions[id]
	if association == nil || association.lastSeen.After(cutoff) {
		r.mu.Unlock()
		return
	}
	delete(r.sessions, id)
	r.mu.Unlock()
	_ = association.conn.Close()
	_ = r.transport.WriteFrame(Frame{Type: FrameClose, AssociationID: id})
}

func (r *TargetRelay) remove(id uint32, notify bool) {
	r.removeAssociation(id, nil, notify)
}

func (r *TargetRelay) removeAssociation(id uint32, expected *targetAssociation, notify bool) bool {
	r.mu.Lock()
	association := r.sessions[id]
	if association == nil || (expected != nil && association != expected) {
		r.mu.Unlock()
		return false
	}
	delete(r.sessions, id)
	r.mu.Unlock()
	_ = association.conn.Close()
	if notify {
		_ = r.transport.WriteFrame(Frame{Type: FrameClose, AssociationID: id})
	}
	return true
}

func (r *TargetRelay) closeAssociations() {
	r.mu.Lock()
	sessions := r.sessions
	r.sessions = make(map[uint32]*targetAssociation)
	r.mu.Unlock()
	for _, association := range sessions {
		_ = association.conn.Close()
	}
	r.wg.Wait()
}

func (r *TargetRelay) reportAssociationError(id uint32, err error) {
	if r.hooks.OnAssociationError != nil {
		r.hooks.OnAssociationError(id, err)
	}
}
