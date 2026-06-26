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

	logger := NewFromEnv()
	if logger == nil {
		t.Fatal("NewFromEnv returned nil")
	}

	logger.Info("discarded message")
}

func TestNewFromEnvWritesConfiguredLog(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "intun.log")
	t.Setenv(EnvLogPath, logPath)

	logger := NewFromEnv()
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
}

func TestNewFromEnvInvalidPathDiscardsSafely(t *testing.T) {
	t.Setenv(EnvLogPath, filepath.Join(t.TempDir(), "missing", "intun.log"))

	logger := NewFromEnv()
	if logger == nil {
		t.Fatal("NewFromEnv returned nil")
	}

	logger.Warn("discarded warning")
}
