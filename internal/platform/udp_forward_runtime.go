package platform

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

type udpForwardRuntime struct {
	session    *ssh.Session
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
		serveErr := r.serve(r.ctx)
		waitErr := r.session.Wait()
		if r.closed.Load() {
			return
		}
		if serveErr != nil {
			r.fail(serveErr)
			return
		}
		if waitErr != nil {
			r.fail(waitErr)
			return
		}
		r.fail(fmt.Errorf("remote UDP relay exited"))
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
		result = r.session.Close()
	})
	return result
}
