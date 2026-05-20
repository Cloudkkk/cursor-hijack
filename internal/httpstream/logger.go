package httpstream

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultLogger implements Logger with configurable verbosity.
type DefaultLogger struct {
	mu       sync.Mutex
	output   io.Writer
	level    LogLevel
	colorize bool
}

// LoggerOption configures a DefaultLogger.
type LoggerOption func(*DefaultLogger)

// WithOutput sets the output writer.
func WithOutput(w io.Writer) LoggerOption {
	return func(l *DefaultLogger) { l.output = w }
}

// WithLevel sets the log level.
func WithLevel(level LogLevel) LoggerOption {
	return func(l *DefaultLogger) { l.level = level }
}

// WithColor enables/disables colorized output.
func WithColor(colorize bool) LoggerOption {
	return func(l *DefaultLogger) { l.colorize = colorize }
}

// NewDefaultLogger creates a new DefaultLogger.
func NewDefaultLogger(opts ...LoggerOption) *DefaultLogger {
	l := &DefaultLogger{
		output:   os.Stdout,
		level:    LogLevelBasic,
		colorize: true,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// ANSI colors
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
)

func (l *DefaultLogger) color(c, s string) string {
	if l.colorize {
		return c + s + colorReset
	}
	return s
}

func (l *DefaultLogger) timestamp() string {
	return time.Now().Format("15:04:05.000")
}

// LogRequest logs an HTTP request.
func (l *DefaultLogger) LogRequest(msg *HTTPMessage) {
	if l.level < LogLevelBasic {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	req := msg.Request
	if req == nil {
		return
	}

	// Basic: timestamp direction method url
	fmt.Fprintf(l.output, "%s %s %s %s%s\n",
		l.color(colorGray, l.timestamp()),
		l.color(colorGreen, "→"),
		l.color(colorCyan, req.Method),
		msg.Host,
		req.URL.RequestURI(),
	)

	// Headers
	if l.level >= LogLevelHeaders {
		for name, values := range req.Header {
			fmt.Fprintf(l.output, "  %s: %s\n",
				l.color(colorYellow, name),
				strings.Join(values, ", "),
			)
		}
	}
}

// LogResponse logs an HTTP response.
func (l *DefaultLogger) LogResponse(msg *HTTPMessage) {
	if l.level < LogLevelBasic {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	resp := msg.Response
	if resp == nil {
		return
	}

	// Status color based on code
	statusColor := colorGreen
	if resp.StatusCode >= 400 {
		statusColor = colorRed
	} else if resp.StatusCode >= 300 {
		statusColor = colorYellow
	}

	// Basic: timestamp direction status content-type
	contentType := resp.Header.Get("Content-Type")
	if idx := strings.Index(contentType, ";"); idx > 0 {
		contentType = contentType[:idx]
	}

	fmt.Fprintf(l.output, "%s %s %s %s [%s]\n",
		l.color(colorGray, l.timestamp()),
		l.color(colorPurple, "←"),
		l.color(statusColor, fmt.Sprintf("%d", resp.StatusCode)),
		msg.Host,
		contentType,
	)

	// Headers
	if l.level >= LogLevelHeaders {
		for name, values := range resp.Header {
			fmt.Fprintf(l.output, "  %s: %s\n",
				l.color(colorYellow, name),
				strings.Join(values, ", "),
			)
		}
	}
}

// LogSSE logs an SSE event.
func (l *DefaultLogger) LogSSE(host string, event *SSEEvent) {
	if l.level < LogLevelDebug {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	eventType := event.Event
	if eventType == "" {
		eventType = "message"
	}

	// Truncate data for display
	data := event.Data
	if len(data) > 200 {
		data = data[:200] + "..."
	}
	data = strings.ReplaceAll(data, "\n", "\\n")

	fmt.Fprintf(l.output, "%s %s %s [%s] %s\n",
		l.color(colorGray, l.timestamp()),
		l.color(colorBlue, "SSE"),
		host,
		l.color(colorCyan, eventType),
		data,
	)
}

// LogBody logs body data chunk.
func (l *DefaultLogger) LogBody(dir Direction, host string, data []byte) {
	if l.level < LogLevelBody {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	arrow := l.color(colorGreen, "→")
	if dir == ServerToClient {
		arrow = l.color(colorPurple, "←")
	}

	// Show first 100 bytes
	preview := data
	if len(preview) > 100 {
		preview = preview[:100]
	}

	// Check if printable
	printable := true
	for _, b := range preview {
		if b < 32 && b != '\n' && b != '\r' && b != '\t' {
			printable = false
			break
		}
	}

	if printable {
		fmt.Fprintf(l.output, "%s %s BODY %s (%d bytes): %s\n",
			l.color(colorGray, l.timestamp()),
			arrow,
			host,
			len(data),
			strings.ReplaceAll(string(preview), "\n", "\\n"),
		)
	} else {
		fmt.Fprintf(l.output, "%s %s BODY %s (%d bytes): <binary>\n",
			l.color(colorGray, l.timestamp()),
			arrow,
			host,
			len(data),
		)
	}
}

// LogGRPC logs a gRPC message.
func (l *DefaultLogger) LogGRPC(msg *GRPCMessage) {
	if l.level < LogLevelBasic {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	arrow := l.color(colorGreen, "→")
	if msg.Direction == ServerToClient {
		arrow = l.color(colorPurple, "←")
	}

	// Truncate JSON for display
	data := msg.JSON
	if len(data) > 200 {
		data = data[:200] + "..."
	}

	if msg.Error != "" {
		fmt.Fprintf(l.output, "%s %s gRPC %s/%s [ERROR: %s]\n",
			l.color(colorGray, l.timestamp()),
			arrow,
			l.color(colorCyan, msg.Service),
			l.color(colorYellow, msg.Method),
			l.color(colorRed, msg.Error),
		)
	} else {
		fmt.Fprintf(l.output, "%s %s gRPC %s/%s %s\n",
			l.color(colorGray, l.timestamp()),
			arrow,
			l.color(colorCyan, msg.Service),
			l.color(colorYellow, msg.Method),
			data,
		)
	}
}

// Debug logs debug information.
func (l *DefaultLogger) Debug(format string, args ...interface{}) {
	if l.level < LogLevelDebug {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	fmt.Fprintf(l.output, "%s %s %s\n",
		l.color(colorGray, l.timestamp()),
		l.color(colorGray, "[DEBUG]"),
		fmt.Sprintf(format, args...),
	)
}
