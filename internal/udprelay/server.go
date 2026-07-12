package udprelay

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	defaultIdleTimeout   = 60 * time.Second
	defaultSweepInterval = 5 * time.Second
)

type RelayOptions struct {
	IdleTimeout     time.Duration
	SweepInterval   time.Duration
	MaxAssociations int
}

type TargetHooks struct {
	OnAssociationError func(id uint32, err error)
}

func (o RelayOptions) withDefaults() RelayOptions {
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = defaultIdleTimeout
	}
	if o.SweepInterval <= 0 {
		o.SweepInterval = defaultSweepInterval
	}
	if o.MaxAssociations <= 0 {
		o.MaxAssociations = DefaultMaxAssociations
	}
	return o
}

func ServeTargetStream(ctx context.Context, input io.Reader, output io.Writer, target string, options RelayOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	transport := NewStreamTransport(input, output, streamClosers(input, output)...)
	relay, err := NewTargetRelay(transport, target, options, TargetHooks{})
	if err != nil {
		_ = transport.Close()
		return err
	}
	if err := transport.WriteFrame(Frame{Type: FrameReady}); err != nil {
		_ = transport.Close()
		return fmt.Errorf("write relay ready frame: %w", err)
	}
	return relay.Serve(ctx)
}

func ServeTarget(ctx context.Context, transport FrameTransport, target string, options RelayOptions, hooks TargetHooks) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	relay, err := NewTargetRelay(transport, target, options, hooks)
	if err != nil {
		return err
	}
	return relay.Serve(ctx)
}

func watchTransportCancellation(ctx context.Context, transport FrameTransport) func() {
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() { close(done) })
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = transport.Close()
		case <-done:
		}
	}()
	return stop
}
