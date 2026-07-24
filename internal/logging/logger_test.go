package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultReturnsLogger(t *testing.T) {
	if Default() == nil {
		t.Fatal("Default returned nil")
	}
}

func TestNewFromEnvWithoutPathDiscardsSafely(t *testing.T) {
	t.Setenv(EnvLogPath, "")

	logger, closer := newFromEnv()
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}
	if logger == nil {
		t.Fatal("newFromEnv returned nil")
	}

	logger.Info("discarded message")
}

func TestNewFromEnvWritesConfiguredLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "intun.log")
	t.Setenv(EnvLogPath, logPath)

	logger, closer := newFromEnv()
	if closer == nil {
		t.Fatal("configured logger should own a file")
	}
	t.Cleanup(func() { _ = closer.Close() })
	logger.Debug("debug detail", "component", "test")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(data)
	if !strings.Contains(logText, "logging initialized") {
		t.Fatalf("log file missing initialization entry:\n%s", logText)
	}
	if !strings.Contains(logText, "debug detail") {
		t.Fatalf("log file missing debug entry:\n%s", logText)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("log mode = %o, want 600", got)
	}
}

func TestNewFromEnvInvalidPathDiscardsSafely(t *testing.T) {
	t.Setenv(EnvLogPath, filepath.Join(t.TempDir(), "missing", "intun.log"))

	logger, closer := newFromEnv()
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}
	if logger == nil {
		t.Fatal("newFromEnv returned nil")
	}

	logger.Warn("discarded warning")
}

func TestNewFromEnvRotatesOversizedLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "intun.log")
	if err := os.WriteFile(logPath, make([]byte, maxLogSize), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvLogPath, logPath)

	_, closer := newFromEnv()
	if closer == nil {
		t.Fatal("rotated logger should own a new file")
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.Stat(logPath + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if backup.Size() != maxLogSize {
		t.Fatalf("rotated log size = %d, want %d", backup.Size(), maxLogSize)
	}
	current, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if current.Size() >= maxLogSize {
		t.Fatalf("new log size = %d, want a fresh log", current.Size())
	}
}
