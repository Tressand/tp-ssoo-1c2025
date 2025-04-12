package logger

import (
	"log/slog"
	"os"
)

var Instance *slog.Logger

func Setup(path string, level slog.Level, override bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_APPEND
	if override {
		flags = flags | os.O_TRUNC
	}

	logFile, err := os.OpenFile(path, flags, 0666)
	if err != nil {
		return err
	}

	Instance = slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: level}))

	// Crear y asignar los argumentos del log.

	slog.SetDefault(Instance)
	return nil
}
