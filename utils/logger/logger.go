package logger

import (
	"io"
	"log/slog"
	"os"
	"ssoo-utils/logger/prettywriter"
)

type Logger = slog.Logger

type LoggerOptions struct {
	Level           slog.Level
	Override        bool
	WriteToTerminal bool
	Pretty          bool
}

var Instance *Logger = slog.Default().With("default", "true")
var file *os.File

func Setup(path string, options LoggerOptions) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_APPEND

	file, err := os.OpenFile(path, flags, 0666)
	if err != nil {
		return err
	}
	if options.Override {
		file.Truncate(0)
		file.WriteString("======= ONLY LAST SESSION LOGS =======\n\n")
	}

	var output io.Writer = file
	if options.WriteToTerminal {
		output = io.MultiWriter(file, os.Stdout)
	}
	if options.Pretty {
		output = prettywriter.NewPrettyWriter(output)
	}
	Instance = slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: options.Level}))
	slog.SetDefault(Instance)

	return nil
}

func SetupDefault(level slog.Level) error {
	return Setup("kernel.log", LoggerOptions{
		Level:           level,
		Override:        true,
		WriteToTerminal: true,
		Pretty:          true,
	})
}

func Close() {
	if file != nil {
		file.Close()
	}
}
