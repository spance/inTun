package udprelay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type PeerHooks struct {
	OnDrop             func(peer *net.UDPAddr, err error)
	OnAssociationError func(id uint32, message string)
}

type PeerRelay struct {
	listener  *net.UDPConn
	transport FrameTransport
	registry  *AssociationRegistry
	options   RelayOptions
	hooks     PeerHooks
	closeOnce sync.Once
}

func NewPeerRelay(listener *net.UDPConn, transport FrameTransport, options RelayOptions, hooks PeerHooks) *PeerRelay {
	options = options.withDefaults()
	return &PeerRelay{
		listener:  listener,
		transport: transport,
		registry:  NewAssociationRegistry(options.MaxAssociations),
		options:   options,
		hooks:     hooks,
	}
}

func (r *PeerRelay) Serve(ctx context.Context) error {
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		errCh <- r.readDatagrams(ctx)
	}()
	go func() {
		defer wg.Done()
		errCh <- r.readFrames(ctx)
	}()

	var result error
	select {
	case <-ctx.Done():
		result = ctx.Err()
	case result = <-errCh:
	}
	_ = r.Close()
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if isClosedError(result) {
		return nil
	}
	return result
}

func (r *PeerRelay) Close() error {
	var result error
	r.closeOnce.Do(func() {
		if err := r.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = err
		}
		if err := r.transport.Close(); err != nil && result == nil && !isClosedError(err) {
			result = err
		}
	})
	return result
}

func (r *PeerRelay) readDatagrams(ctx context.Context) error {
	buffer := make([]byte, MaxDatagramSize)
	for {
		_ = r.listener.SetReadDeadline(time.Now().Add(r.options.SweepInterval))
		n, peer, err := r.listener.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if err := r.expireAssociations(time.Now()); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("read UDP peer datagram: %w", err)
		}
		associationID, err := r.registry.Resolve(peer, time.Now())
		if err != nil {
			if r.hooks.OnDrop != nil {
				r.hooks.OnDrop(peer, err)
			}
			continue
		}
		payload := append([]byte(nil), buffer[:n]...)
		if err := r.transport.WriteFrame(Frame{Type: FrameData, AssociationID: associationID, Payload: payload}); err != nil {
			return fmt.Errorf("write peer relay frame: %w", err)
		}
	}
}

func (r *PeerRelay) readFrames(ctx context.Context) error {
	for {
		frame, err := r.transport.ReadFrame()
		if err != nil {
			if ctx.Err() != nil || isClosedError(err) {
				return nil
			}
			return fmt.Errorf("read peer relay frame: %w", err)
		}
		switch frame.Type {
		case FrameData:
			peer, ok := r.registry.Peer(frame.AssociationID, time.Now())
			if !ok {
				continue
			}
			if _, err := r.listener.WriteToUDP(frame.Payload, peer); err != nil {
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					return nil
				}
				return fmt.Errorf("write UDP peer datagram: %w", err)
			}
		case FrameClose:
			r.registry.DeleteIfLastSeenBefore(frame.AssociationID, time.Now().Add(-r.options.IdleTimeout))
		case FrameError:
			r.registry.Delete(frame.AssociationID)
			if r.hooks.OnAssociationError != nil {
				r.hooks.OnAssociationError(frame.AssociationID, string(frame.Payload))
			}
		default:
			return fmt.Errorf("%w: unexpected peer frame type %d", ErrInvalidFrame, frame.Type)
		}
	}
}

func (r *PeerRelay) expireAssociations(now time.Time) error {
	for _, id := range r.registry.ExpireBefore(now.Add(-r.options.IdleTimeout)) {
		if err := r.transport.WriteFrame(Frame{Type: FrameClose, AssociationID: id}); err != nil {
			return fmt.Errorf("close peer association: %w", err)
		}
	}
	return nil
}
