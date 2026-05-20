package httpstream

import (
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
)

// BodyDecoder handles Content-Encoding decoding.
type BodyDecoder struct{}

// NewBodyDecoder creates a new BodyDecoder.
func NewBodyDecoder() *BodyDecoder {
	return &BodyDecoder{}
}

// Decode wraps the body with appropriate decoders based on Content-Encoding.
// Returns a streaming io.Reader that decodes on-the-fly.
func (d *BodyDecoder) Decode(body io.Reader, headers http.Header) io.Reader {
	if body == nil {
		return nil
	}

	reader := body

	// Content-Encoding can have multiple values, decode in order
	encodings := parseContentEncoding(headers.Get("Content-Encoding"))

	for _, encoding := range encodings {
		switch strings.ToLower(strings.TrimSpace(encoding)) {
		case "gzip", "x-gzip":
			if gr, err := gzip.NewReader(reader); err == nil {
				reader = gr
			}
		case "deflate":
			reader = flate.NewReader(reader)
		case "br":
			reader = brotli.NewReader(reader)
		// identity, chunked don't need processing
		}
	}

	return reader
}

// parseContentEncoding parses Content-Encoding header value.
func parseContentEncoding(value string) []string {
	if value == "" {
		return nil
	}
	// "gzip, br" → ["gzip", "br"]
	parts := strings.Split(value, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && p != "identity" {
			result = append(result, p)
		}
	}
	return result
}

// BodyReader provides streaming body reading with automatic decoding.
type BodyReader struct {
	raw     io.Reader
	decoded io.Reader
	size    int64
	isSSE   bool
	headers http.Header
}

// NewBodyReader creates a new BodyReader with automatic decoding.
func NewBodyReader(body io.Reader, headers http.Header) *BodyReader {
	if body == nil {
		return nil
	}

	br := &BodyReader{
		raw:     body,
		size:    -1,
		headers: headers,
	}

	// Parse Content-Length
	if cl := headers.Get("Content-Length"); cl != "" {
		br.size, _ = strconv.ParseInt(cl, 10, 64)
	}

	// Detect SSE
	ct := headers.Get("Content-Type")
	br.isSSE = strings.Contains(ct, "text/event-stream")

	// Decode Content-Encoding
	br.decoded = NewBodyDecoder().Decode(body, headers)

	return br
}

// Read implements io.Reader for streaming reads.
func (br *BodyReader) Read(p []byte) (int, error) {
	if br.decoded == nil {
		return 0, io.EOF
	}
	return br.decoded.Read(p)
}

// ReadChunk reads a chunk of specified size.
func (br *BodyReader) ReadChunk(size int) ([]byte, error) {
	buf := make([]byte, size)
	n, err := br.decoded.Read(buf)
	return buf[:n], err
}

// IsSSE returns true if this is an SSE stream.
func (br *BodyReader) IsSSE() bool {
	return br.isSSE
}

// SSE returns an SSE parser for this body (only valid if IsSSE is true).
func (br *BodyReader) SSE(opts ...SSEOption) *SSEParser {
	return NewSSEParser(br.decoded, opts...)
}

// Size returns Content-Length or -1 if unknown/chunked.
func (br *BodyReader) Size() int64 {
	return br.size
}

// ContentType returns the Content-Type header value.
func (br *BodyReader) ContentType() string {
	return br.headers.Get("Content-Type")
}

// Headers returns the original headers.
func (br *BodyReader) Headers() http.Header {
	return br.headers
}

// ReadAll reads the entire body (non-streaming wrapper).
func (br *BodyReader) ReadAll() ([]byte, error) {
	return io.ReadAll(br.decoded)
}

// ReadAllWithLimit reads the body with a size limit.
func (br *BodyReader) ReadAllWithLimit(limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(br.decoded, limit))
}

// Close closes any underlying readers if they implement io.Closer.
func (br *BodyReader) Close() error {
	if closer, ok := br.decoded.(io.Closer); ok {
		return closer.Close()
	}
	if closer, ok := br.raw.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
