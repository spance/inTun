package platform

import (
	"errors"
	"testing"
)

func TestParseFailure(t *testing.T) {
	got := ParseFailure("SSH_CONNECTION_FAILED: host: connection refused")
	if got.Code != FailureSSHConnection || got.Detail != "host: connection refused" {
		t.Fatalf("ParseFailure() = %#v", got)
	}

	unknown := ParseFailure("plain failure")
	if unknown.Code != FailureUnknown || unknown.Detail != "plain failure" {
		t.Fatalf("unknown failure = %#v", unknown)
	}
}

func TestFailureFromErrorPreservesTypedFailure(t *testing.T) {
	input := NewFailure(FailureUDPRelay, "startup", errors.New("not installed"))
	got := FailureFromError(input)
	if got == input {
		t.Fatal("FailureFromError returned the mutable input pointer")
	}
	if got.Code != FailureUDPRelay || got.Op != "startup" || got.Detail != "not installed" {
		t.Fatalf("FailureFromError() = %#v", got)
	}
}
