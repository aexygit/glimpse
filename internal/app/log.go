package app

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

const maxLogBytes = 5 << 20

// SetupLog appends to dir/glimpse.log, rotating it to glimpse.old.log first when over 5 MiB.
// ponytail: rotation on startup only; switch to size-based rotation if long sessions produce huge logs.
func SetupLog(dir string) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, "glimpse.log")
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogBytes {
		if err := os.Rename(path, filepath.Join(dir, "glimpse.old.log")); err != nil {
			return nil, nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}
	level := slog.LevelInfo
	if os.Getenv("GLIMPSE_DEBUG") == "1" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})), f, nil
}
