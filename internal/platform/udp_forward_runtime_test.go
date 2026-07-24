package platform

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeUDPRemoteSession struct {
	waitErr error
	closed  chan struct{}
	once    sync.Once
}

func newFakeUDPRemoteSession(waitErr error) *fakeUDPRemoteSession {
	return &fakeUDPRemoteSession{waitErr: waitErr, closed: make(chan struct{})}
}

func (s *fakeUDPRemoteSession) Wait() error {
	<-s.closed
	return s.waitErr
}

func (s *fakeUDPRemoteSession) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func TestUDPForwardRuntimeReportsServeFailureImmediately(t *testing.T) {
	session := newFakeUDPRemoteSession(context.Canceled)
	want := errors.New("peer relay failed")
	failed := make(chan error, 1)
	runtime := newUDPForwardRuntimeWithSession(session, nil, "", func(context.Context) error {
		return want
	}, func(err error) {
		failed <- err
	})

	runtime.start()
	select {
	case got := <-failed:
		if !errors.Is(got, want) {
			t.Fatalf("failure = %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("serve failure was blocked by session.Wait")
	}
}

func TestUDPForwardRuntimeReportsSessionFailureAndCancelsServe(t *testing.T) {
	want := errors.New("remote relay exited")
	session := newFakeUDPRemoteSession(want)
	failed := make(chan error, 1)
	serveDone := make(chan struct{})
	runtime := newUDPForwardRuntimeWithSession(session, nil, "", func(ctx context.Context) error {
		<-ctx.Done()
		close(serveDone)
		return ctx.Err()
	}, func(err error) {
		failed <- err
	})

	runtime.start()
	_ = session.Close()
	select {
	case got := <-failed:
		if !errors.Is(got, want) {
			t.Fatalf("failure = %v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("session failure was not reported")
	}
	select {
	case <-serveDone:
	case <-time.After(time.Second):
		t.Fatal("serve context was not cancelled")
	}
}
