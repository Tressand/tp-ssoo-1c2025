package prettywriter

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

type PrettyWriter struct {
	output io.Writer
}

var mutex sync.Mutex

func NewPrettyWriter(output io.Writer) *PrettyWriter {
	return &PrettyWriter{
		output: output,
	}
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
	mutex.Lock()
	n, err = cw.output.Write([]byte(prettyEntry))
	if err != nil {
		return n, fmt.Errorf("failed to write custom log: %w", err)
	}
	mutex.Unlock()
	return len(p), nil // Return the original byte count to satisfy the logger
}

func prettyfy(entry map[string]any) string {
	var logTime time.Time
	var err error
	logTime, err = time.Parse("2006-01-02T15:04:05.999999999Z", fmt.Sprint(entry["time"]))
	if err != nil {
		logTime, err = time.Parse("2006-01-02T15:04:05.999999999-07:00", fmt.Sprint(entry["time"]))
		if err != nil {
			fmt.Println("time conversion failed!")
			fmt.Println(fmt.Sprint(entry["time"]))
			fmt.Println(err.Error())
		}
	}
	var str string
	str += fmt.Sprintf("[%s]", logTime.Format("15:04:05.9999"))
	if name, ok := entry["name"]; ok {
		str += fmt.Sprintf(" (%v)", name)
		delete(entry, "name")
	}
	str += fmt.Sprintf(" (%s) %s", entry["level"], entry["msg"])
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

func (cw *PrettyWriter) WriteString(str string) (n int, err error) {
	mutex.Lock()
	n, err = cw.output.Write([]byte(str))
	if err != nil {
		return n, fmt.Errorf("failed to write string to log: %w", err)
	}
	mutex.Unlock()
	return len(str), nil
}
