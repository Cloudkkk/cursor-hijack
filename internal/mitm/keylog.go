package mitm

import (
	"os"
	"sync"
)

// KeyLogWriter writes TLS key log in NSS Key Log Format for Wireshark.
type KeyLogWriter struct {
	file *os.File
	mu   sync.Mutex
}

// NewKeyLogWriter creates a new key log writer.
func NewKeyLogWriter(path string) (*KeyLogWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_SYNC, 0600)
	if err != nil {
		return nil, err
	}
	return &KeyLogWriter{file: file}, nil
}

// Write implements io.Writer for KeyLogWriter.
func (w *KeyLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Write(p)
}

// Close closes the key log file.
func (w *KeyLogWriter) Close() error {
	return w.file.Close()
}
