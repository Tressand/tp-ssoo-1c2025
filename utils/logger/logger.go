package logger

import (
	"fmt"
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
var customWriter *prettywriter.PrettyWriter

func Setup(path string, options LoggerOptions) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_APPEND

	file, err := os.OpenFile(path, flags, 0666)
	if err != nil {
		return err
	}
	if options.Override {
		file.Truncate(0)
		file.WriteString("======= LOGS DE LA ÚLTIMA SESIÓN =======\n\n")
	}

	var output io.Writer = file
	if options.WriteToTerminal {
		output = io.MultiWriter(file, os.Stdout)
	}
	if options.Pretty {
		customWriter = prettywriter.NewPrettyWriter(output)
		output = customWriter
	}
	Instance = slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: options.Level}))
	slog.SetDefault(Instance)

	return nil
}

func SetupDefault(name string, level slog.Level) error {
	return Setup(name+".log", LoggerOptions{
		Level:           level,
		Override:        true,
		WriteToTerminal: true,
		Pretty:          true,
	})
}

/*
(prefixed) añade el prefijo "##" a los logs que lo requieren según consigna.

(pid) cero para omitir el pid.

(msg) vacio es posible.

todos los pares agregados al mapa (variables) se agregan en formato "{clave}: {valor}".

todos los valores, excepto el prefijo, se separan automáticamente con " - " como pide la consigna.
*/
func RequiredLog(prefixed bool, pid uint, msg string, variables map[string]string) {
	if customWriter == nil {
		slog.Error("logger sin inicializar o modo pretty desactivado, activar para los realizar los logs obligatorios")
		return
	}
	var str string
	if prefixed {
		str = "## "
	}
	if pid != 0 {
		str += fmt.Sprintf("PID: %d", pid)
	}
	if msg != "" {
		str += " - " + msg
	}
	for key, value := range variables {
		str += fmt.Sprintf(" - %s: %s", key, value)
	}
	customWriter.WriteString(str + "\n")
}

func Close() {
	if file != nil {
		file.Close()
	}
}
