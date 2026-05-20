package httpstream

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
	"strings"
)

// SSEParser provides streaming SSE event parsing.
// Compatible with non-standard SSE implementations.
type SSEParser struct {
	reader *bufio.Reader
	lastID string
	strict bool
}

// SSEOption configures an SSEParser.
type SSEOption func(*SSEParser)

// WithStrict enables strict SSE parsing mode.
func WithStrict(strict bool) SSEOption {
	return func(p *SSEParser) { p.strict = strict }
}

// NewSSEParser creates a new streaming SSE parser.
func NewSSEParser(r io.Reader, opts ...SSEOption) *SSEParser {
	p := &SSEParser{
		reader: bufio.NewReader(r),
		strict: false, // Default: lenient mode for non-standard SSE
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Next reads and returns the next SSE event (streaming interface).
func (p *SSEParser) Next() (*SSEEvent, error) {
	var event SSEEvent
	var rawLines [][]byte
	hasData := false

	for {
		line, err := p.reader.ReadBytes('\n')
		if err != nil {
			// EOF: return accumulated event if any
			if hasData {
				event.Raw = bytes.Join(rawLines, []byte("\n"))
				return &event, nil
			}
			return nil, err
		}

		rawLines = append(rawLines, line)

		// Trim line endings (\r\n or \n)
		line = bytes.TrimSuffix(line, []byte("\n"))
		line = bytes.TrimSuffix(line, []byte("\r"))

		// Empty line = event separator
		if len(line) == 0 {
			if hasData {
				event.Data = strings.TrimSuffix(event.Data, "\n")
				event.Raw = bytes.Join(rawLines, []byte("\n"))
				if event.ID == "" {
					event.ID = p.lastID
				}
				return &event, nil
			}
			rawLines = nil // Reset for next event
			continue
		}

		// Comment line (starts with :)
		if line[0] == ':' {
			continue
		}

		// Parse field
		field, value := parseSSEField(line)

		switch field {
		case "data":
			event.Data += value + "\n"
			hasData = true
		case "event":
			event.Event = value
		case "id":
			// Spec: id must not contain NULL
			if !bytes.Contains([]byte(value), []byte{0}) {
				event.ID = value
				p.lastID = value
			}
		case "retry":
			if n, err := strconv.Atoi(value); err == nil && n >= 0 {
				event.Retry = n
			}
		default:
			// Non-standard field: ignore in lenient mode
			if p.strict {
				// Could log warning here
			}
		}
	}
}

// parseSSEField parses an SSE field line.
// Standard: "field: value" or "field:value"
// Non-standard: "field value" (some implementations)
func parseSSEField(line []byte) (field, value string) {
	// Look for : separator
	idx := bytes.IndexByte(line, ':')
	if idx == -1 {
		// Non-standard format: possibly "field value"
		parts := bytes.SplitN(line, []byte(" "), 2)
		if len(parts) == 2 {
			return string(parts[0]), string(bytes.TrimSpace(parts[1]))
		}
		return string(line), ""
	}

	field = string(line[:idx])
	value = string(line[idx+1:])

	// Spec: if : is followed by a space, skip it
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}

	return field, value
}

// ReadAll reads all events (non-streaming wrapper).
func (p *SSEParser) ReadAll() ([]SSEEvent, error) {
	var events []SSEEvent
	for {
		event, err := p.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return events, err
		}
		events = append(events, *event)
	}
	return events, nil
}

// Chan returns a channel that receives events (async streaming).
func (p *SSEParser) Chan() <-chan SSEEvent {
	ch := make(chan SSEEvent)
	go func() {
		defer close(ch)
		for {
			event, err := p.Next()
			if err != nil {
				break
			}
			ch <- *event
		}
	}()
	return ch
}

// LastEventID returns the last event ID seen.
func (p *SSEParser) LastEventID() string {
	return p.lastID
}
