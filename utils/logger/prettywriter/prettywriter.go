package prettywriter

import (
	"encoding/json"
	"fmt"
	"io"
)

type PrettyWriter struct {
	output io.Writer
}

func (cw *PrettyWriter) Write(p []byte) (n int, err error) {
	// Parse the JSON log entry
	var logEntry map[string]interface{}
	if err := json.Unmarshal(p, &logEntry); err != nil {
		return 0, fmt.Errorf("failed to parse log entry: %w", err)
	}

	// Convert the log entry to your custom format
	prettyEntry := prettyfy(logEntry)

	// Write the custom format to the output
	n, err = cw.output.Write([]byte(prettyEntry))
	if err != nil {
		return n, fmt.Errorf("failed to write custom log: %w", err)
	}

	return len(p), nil // Return the original byte count to satisfy the logger
}

func NewPrettyWriter(output io.Writer) *PrettyWriter {
	return &PrettyWriter{
		output: output,
	}
}

func prettyfy(entry map[string]any) string {
	var str string = fmt.Sprintf("[%s] (%s) %s", entry["time"], entry["level"], entry["msg"])
	delete(entry, "time")
	delete(entry, "level")
	delete(entry, "msg")
	if len(entry) == 0 {
		str += "\n"
		return str
	}
	str += " |"
	for key, value := range entry {
		str += fmt.Sprintf(" %s: %v |", key, value)
	}
	str += "\n"
	return str
}
