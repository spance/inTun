package platform

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type globalRequestFunc func(name string, wantReply bool, payload []byte) (bool, []byte, error)

func (f globalRequestFunc) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	return f(name, wantReply, payload)
}

func TestSendGlobalRequestWithTimeout(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		requester := globalRequestFunc(func(name string, wantReply bool, _ []byte) (bool, []byte, error) {
			if name != "keepalive@openssh.org" || !wantReply {
				return false, nil, errors.New("unexpected keepalive request")
			}
			return true, nil, nil
		})
		if latency, err := sendGlobalRequestWithTimeout(requester, nil, time.Second); err != nil || latency <= 0 {
			t.Fatalf("latency=%v err=%v", latency, err)
		}
	})

	t.Run("error", func(t *testing.T) {
		want := errors.New("request failed")
		requester := globalRequestFunc(func(string, bool, []byte) (bool, []byte, error) {
			return false, nil, want
		})
		if _, err := sendGlobalRequestWithTimeout(requester, nil, time.Second); !errors.Is(err, want) {
			t.Fatalf("error = %v, want %v", err, want)
		}
	})

	t.Run("timeout closes transport", func(t *testing.T) {
		release := make(chan struct{})
		requester := globalRequestFunc(func(string, bool, []byte) (bool, []byte, error) {
			<-release
			return false, nil, errors.New("closed")
		})
		var closed atomic.Bool
		var once sync.Once
		closeRequest := func() error {
			closed.Store(true)
			once.Do(func() { close(release) })
			return nil
		}
		if _, err := sendGlobalRequestWithTimeout(requester, closeRequest, 10*time.Millisecond); err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("timeout error = %v", err)
		}
		if !closed.Load() {
			t.Fatal("timeout did not close the request transport")
		}
	})
}
