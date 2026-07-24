package main

import (
	"os"
	"strings"
	"testing"
)

func TestRunRejectsInvalidRelayCommand(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"intun", "relay"}

	err := run()
	if err == nil || !strings.Contains(err.Error(), "UDP relay failed") {
		t.Fatalf("run error = %v, want relay context", err)
	}
}
