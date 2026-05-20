package mitm

import (
	"bufio"
	"io"
	"net"
)

// TLS Record Types
const (
	tlsRecordTypeChangeCipherSpec = 20
	tlsRecordTypeAlert            = 21
	tlsRecordTypeHandshake        = 22 // 0x16
	tlsRecordTypeApplicationData  = 23
)

// TLS Versions
const (
	tlsVersion10 = 0x0301
	tlsVersion11 = 0x0302
	tlsVersion12 = 0x0303
	tlsVersion13 = 0x0304
)

// PeekableConn wraps a net.Conn with peek capability.
type PeekableConn struct {
	net.Conn
	reader *bufio.Reader
}

// NewPeekableConn creates a new PeekableConn.
func NewPeekableConn(conn net.Conn) *PeekableConn {
	return &PeekableConn{
		Conn:   conn,
		reader: bufio.NewReader(conn),
	}
}

// Peek returns the next n bytes without advancing the reader.
func (c *PeekableConn) Peek(n int) ([]byte, error) {
	return c.reader.Peek(n)
}

// Read reads data from the connection.
func (c *PeekableConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

// IsTLSClientHello checks if the data starts with a TLS ClientHello.
func IsTLSClientHello(data []byte) bool {
	if len(data) < 6 {
		return false
	}

	if data[0] != tlsRecordTypeHandshake {
		return false
	}

	version := uint16(data[1])<<8 | uint16(data[2])
	if version < tlsVersion10 || version > tlsVersion13 {
		if version != 0x0300 {
			return false
		}
	}

	if data[5] != 0x01 {
		return false
	}

	return true
}

// DetectTLS peeks at the connection to determine if it's TLS.
func DetectTLS(conn *PeekableConn) (bool, error) {
	data, err := conn.Peek(6)
	if err != nil {
		if err == io.EOF {
			return false, nil
		}
		return false, err
	}

	return IsTLSClientHello(data), nil
}

// DetectTLSWithSNI peeks at the connection to detect TLS and extract SNI.
func DetectTLSWithSNI(conn *PeekableConn) (bool, string, error) {
	data, err := conn.Peek(6)
	if err != nil {
		if err == io.EOF {
			return false, "", nil
		}
		return false, "", err
	}

	if !IsTLSClientHello(data) {
		return false, "", nil
	}

	recordLen := int(data[3])<<8 | int(data[4])
	totalLen := 5 + recordLen

	if totalLen > 16384 {
		totalLen = 16384
	}

	fullData, err := conn.Peek(totalLen)
	if err != nil && err != io.EOF {
		fullData, _ = conn.Peek(conn.reader.Buffered())
	}

	sni := extractSNI(fullData)
	return true, sni, nil
}

// extractSNI extracts the Server Name Indication from a TLS ClientHello.
func extractSNI(data []byte) string {
	dataLen := len(data)

	if dataLen < 43 {
		return ""
	}

	pos := 5  // Skip TLS record header
	pos += 4  // Skip handshake header
	pos += 2  // Skip client version
	pos += 32 // Skip random

	// Session ID
	if pos >= dataLen {
		return ""
	}
	sessionIDLen := int(data[pos])
	pos++
	if pos+sessionIDLen > dataLen {
		return ""
	}
	pos += sessionIDLen

	// Cipher Suites
	if pos+2 > dataLen {
		return ""
	}
	cipherSuitesLen := int(data[pos])<<8 | int(data[pos+1])
	pos += 2
	if pos+cipherSuitesLen > dataLen {
		return ""
	}
	pos += cipherSuitesLen

	// Compression Methods
	if pos >= dataLen {
		return ""
	}
	compressionMethodsLen := int(data[pos])
	pos++
	if pos+compressionMethodsLen > dataLen {
		return ""
	}
	pos += compressionMethodsLen

	// Extensions
	if pos+2 > dataLen {
		return ""
	}
	extensionsLen := int(data[pos])<<8 | int(data[pos+1])
	pos += 2

	extensionsEnd := pos + extensionsLen
	if extensionsEnd > dataLen {
		extensionsEnd = dataLen
	}

	// Iterate through ALL extensions to find SNI (type 0x0000)
	for pos+4 <= extensionsEnd {
		extType := int(data[pos])<<8 | int(data[pos+1])
		extLen := int(data[pos+2])<<8 | int(data[pos+3])
		pos += 4

		if extLen < 0 || pos+extLen > extensionsEnd {
			break
		}

		if extType == 0 && extLen > 0 {
			sni := parseSNIExtension(data[pos : pos+extLen])
			if sni != "" {
				return sni
			}
		}

		pos += extLen
	}

	return ""
}

// parseSNIExtension parses the SNI extension data.
func parseSNIExtension(data []byte) string {
	dataLen := len(data)
	if dataLen < 5 {
		return ""
	}

	listLen := int(data[0])<<8 | int(data[1])
	pos := 2

	listEnd := pos + listLen
	if listEnd > dataLen {
		listEnd = dataLen
	}

	for pos+3 <= listEnd {
		nameType := data[pos]
		pos++

		if pos+2 > listEnd {
			break
		}
		nameLen := int(data[pos])<<8 | int(data[pos+1])
		pos += 2

		if nameLen <= 0 || pos+nameLen > listEnd {
			break
		}

		if nameType == 0 {
			hostname := string(data[pos : pos+nameLen])
			if isValidHostname(hostname) {
				return hostname
			}
		}

		pos += nameLen
	}

	return ""
}

// isValidHostname performs basic validation on extracted hostname.
func isValidHostname(s string) bool {
	if len(s) == 0 || len(s) > 255 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_') {
			return false
		}
	}
	return true
}

// Protocol represents detected application protocol.
type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolTLS
	ProtocolPlain
)

func (p Protocol) String() string {
	switch p {
	case ProtocolTLS:
		return "TLS"
	case ProtocolPlain:
		return "Plain"
	default:
		return "Unknown"
	}
}
