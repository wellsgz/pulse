package logging

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"time"
)

// Format represents the logging output format
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// Logger wraps the standard logger with format options
type Logger struct {
	format Format
	writer io.Writer
}

// Global logger instance
var defaultLogger = &Logger{
	format: FormatText,
	writer: os.Stderr,
}

// SetFormat sets the logging format globally
func SetFormat(format Format) {
	defaultLogger.format = format
}

// SetWriter sets the output writer
func SetWriter(w io.Writer) {
	defaultLogger.writer = w
	log.SetOutput(w)
}

// LogEntry represents a structured log entry for JSON output
type LogEntry struct {
	Timestamp string      `json:"timestamp"`
	Level     string      `json:"level"`
	Component string      `json:"component"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
}

// ProbeLogEntry represents a probe result log entry
type ProbeLogEntry struct {
	Timestamp string  `json:"timestamp"`
	Level     string  `json:"level"`
	Component string  `json:"component"`
	Target    string  `json:"target"`
	LatencyMs float64 `json:"latency_ms"`
	Success   bool    `json:"success"`
	Error     string  `json:"error,omitempty"`
}

// Info logs an info message
func Info(component, message string, data interface{}) {
	if defaultLogger.format == FormatJSON {
		entry := LogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Level:     "info",
			Component: component,
			Message:   message,
			Data:      data,
		}
		jsonBytes, _ := json.Marshal(entry)
		defaultLogger.writer.Write(append(jsonBytes, '\n'))
	} else {
		log.Printf("[%s] %s", component, message)
	}
}

// ProbeResult logs a probe result
func ProbeResult(target string, latencyMs float64, success bool, errMsg string) {
	if defaultLogger.format == FormatJSON {
		entry := ProbeLogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Level:     "info",
			Component: "Probe",
			Target:    target,
			LatencyMs: latencyMs,
			Success:   success,
			Error:     errMsg,
		}
		jsonBytes, _ := json.Marshal(entry)
		defaultLogger.writer.Write(append(jsonBytes, '\n'))
	} else {
		if success {
			log.Printf("[Probe] %s: %.2fms", target, latencyMs)
		} else {
			log.Printf("[Probe] %s: FAILED - %s", target, errMsg)
		}
	}
}

// Error logs an error message
func Error(component, message string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}

	if defaultLogger.format == FormatJSON {
		entry := LogEntry{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Level:     "error",
			Component: component,
			Message:   message,
			Data:      map[string]string{"error": errStr},
		}
		jsonBytes, _ := json.Marshal(entry)
		defaultLogger.writer.Write(append(jsonBytes, '\n'))
	} else {
		if err != nil {
			log.Printf("[%s] %s: %v", component, message, err)
		} else {
			log.Printf("[%s] %s", component, message)
		}
	}
}

// GetFormat returns the current logging format
func GetFormat() Format {
	return defaultLogger.format
}
