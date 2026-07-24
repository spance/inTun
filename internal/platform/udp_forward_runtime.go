package platform

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

type udpRemoteSession interface {
	Wait() error
	Close() error
}

type udpForwardRuntime struct {
	session    udpRemoteSession
	serve      func(context.Context) error
	listener   *net.UDPConn
	remoteAddr string
	onFailure  func(error)
	ctx        context.Context
	cancel     context.CancelFunc
	closed     atomic.Bool
	closeOnce  sync.Once
	failOnce   sync.Once
}

func newUDPForwardRuntime(session *ssh.Session, listener *net.UDPConn, remoteAddr string, serve func(context.Context) error, onFailure func(error)) *udpForwardRuntime {
	return newUDPForwardRuntimeWithSession(session, listener, remoteAddr, serve, onFailure)
}

func newUDPForwardRuntimeWithSession(session udpRemoteSession, listener *net.UDPConn, remoteAddr string, serve func(context.Context) error, onFailure func(error)) *udpForwardRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	return &udpForwardRuntime{
		session:    session,
		serve:      serve,
		listener:   listener,
		remoteAddr: remoteAddr,
		onFailure:  onFailure,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (r *udpForwardRuntime) start() {
	go func() {
		results := make(chan error, 2)
		go func() { results <- r.serve(r.ctx) }()
		go func() { results <- r.session.Wait() }()

		first := <-results
		if !r.closed.Load() {
			if first == nil {
				first = fmt.Errorf("UDP relay runtime exited")
			}
			r.fail(first)
		}
		<-results
	}()
}

func (r *udpForwardRuntime) fail(err error) {
	if err == nil || r.closed.Load() {
		return
	}
	r.failOnce.Do(func() {
		if r.onFailure != nil {
			r.onFailure(err)
		}
		_ = r.Close()
	})
}

func (r *udpForwardRuntime) Close() error {
	var result error
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		r.cancel()
		if r.listener != nil {
			_ = r.listener.Close()
		}
		if r.session != nil {
			result = r.session.Close()
		}
	})
	return result
}
