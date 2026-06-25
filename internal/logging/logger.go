package logging

import (
	"io"
	"log/slog"
	"os"
)

const EnvLogPath = "INTUN_LOG"

var defaultLogger = NewFromEnv()

func Default() *slog.Logger {
	return defaultLogger
}

func NewFromEnv() *slog.Logger {
	path := os.Getenv(EnvLogPath)
	if path == "" {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	logger.Info("logging initialized", "path", path)
	return logger
}
