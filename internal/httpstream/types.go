// Package httpstream provides streaming HTTP parsing for MITM proxy.
package httpstream

import (
	"io"
	"net/http"
	"time"
)

// Direction indicates the data flow direction.
type Direction int

const (
	ClientToServer Direction = iota
	ServerToClient
)

func (d Direction) String() string {
	if d == ClientToServer {
		return "C2S"
	}
	return "S2C"
}

// HTTPMessage represents a parsed HTTP message.
type HTTPMessage struct {
	Direction Direction
	Request   *http.Request
	Response  *http.Response
	Body      *BodyReader
	Host      string
	Timestamp time.Time
}

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	ID    string
	Event string
	Data  string
	Retry int
	Raw   []byte // Original data for non-standard formats
}

// LogLevel controls logging verbosity.
type LogLevel int

const (
	LogLevelNone LogLevel = iota
	LogLevelBasic
	LogLevelHeaders
	LogLevelBody
	LogLevelDebug
)

// Logger interface for HTTP stream logging.
type Logger interface {
	// LogRequest logs an HTTP request.
	LogRequest(msg *HTTPMessage)
	// LogResponse logs an HTTP response.
	LogResponse(msg *HTTPMessage)
	// LogSSE logs an SSE event.
	LogSSE(host string, event *SSEEvent)
	// LogBody logs body data chunk.
	LogBody(direction Direction, host string, data []byte)
	// LogGRPC logs a gRPC message.
	LogGRPC(msg *GRPCMessage)
	// Debug logs debug information.
	Debug(format string, args ...interface{})
}

// NopLogger is a no-op logger.
type NopLogger struct{}

func (NopLogger) LogRequest(msg *HTTPMessage)                  {}
func (NopLogger) LogResponse(msg *HTTPMessage)                 {}
func (NopLogger) LogSSE(host string, event *SSEEvent)          {}
func (NopLogger) LogBody(dir Direction, host string, _ []byte) {}
func (NopLogger) LogGRPC(msg *GRPCMessage)                     {}
func (NopLogger) Debug(format string, args ...interface{})     {}

// Closer interface for resources that need cleanup.
type Closer interface {
	io.Closer
}
