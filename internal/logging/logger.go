package logging

import (
	"io"
	"log/slog"
	"os"
	"sync"
)

const EnvLogPath = "INTUN_LOG"

const maxLogSize = 5 << 20

var (
	defaultLogger *slog.Logger
	defaultCloser io.Closer
	closeOnce     sync.Once
)

func init() {
	defaultLogger, defaultCloser = newFromEnv()
}

func Default() *slog.Logger {
	return defaultLogger
}

func newFromEnv() (*slog.Logger, io.Closer) {
	path := os.Getenv(EnvLogPath)
	if path == "" {
		return discardLogger(), nil
	}

	if err := rotateLog(path); err != nil {
		return discardLogger(), nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return discardLogger(), nil
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return discardLogger(), nil
	}

	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	logger.Info("logging initialized", "path", path)
	return logger, f
}

func Close() error {
	var err error
	closeOnce.Do(func() {
		if defaultCloser != nil {
			err = defaultCloser.Close()
		}
	})
	return err
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func rotateLog(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() < maxLogSize {
		return nil
	}
	backup := path + ".1"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(path, backup)
}
