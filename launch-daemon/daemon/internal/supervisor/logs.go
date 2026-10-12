// Package supervisor manages the lifecycle of supervised processes.
package supervisor

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	ringCap      = 1000   // max lines in memory ring buffer
	maxLineLen   = 4096   // max bytes per line before truncation
	fileFlushBuf = 65536  // 64 KB bufio writer
	maxFileSize  = 10e6   // 10 MB rotation threshold
	keepBackups  = 1      // keep .1 rotation file
)

// RingBuffer holds the last N lines of output.
type RingBuffer struct {
	mu    sync.Mutex
	lines []string
	pos   int
	full  bool
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		lines: make([]string, 0, capacity),
	}
}

// Add appends a line to the ring buffer.
func (r *RingBuffer) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.lines) < cap(r.lines) {
		r.lines = append(r.lines, line)
	} else {
		r.lines[r.pos] = line
		r.pos = (r.pos + 1) % cap(r.lines)
		r.full = true
	}
}

// Tail returns the last n lines from the ring buffer.
func (r *RingBuffer) Tail(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	total := len(r.lines)
	if n > total {
		n = total
	}

	result := make([]string, n)
	if !r.full {
		copy(result, r.lines[total-n:])
	} else {
		start := (r.pos - n + cap(r.lines)) % cap(r.lines)
		for i := range n {
			result[i] = r.lines[(start+i)%cap(r.lines)]
		}
	}
	return result
}

// Len returns the number of lines in the buffer.
func (r *RingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.lines)
}

// FileWriter writes log lines to a file with rotation.
type FileWriter struct {
	mu     sync.Mutex
	dir    string
	name   string
	file   *os.File
	writer *bufio.Writer
	size   int64
	closed bool
}

// NewFileWriter creates a log file writer.
func NewFileWriter(dir, name string) (*FileWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir logs: %w", err)
	}

	fw := &FileWriter{dir: dir, name: name}
	if err := fw.open(); err != nil {
		return nil, err
	}
	return fw, nil
}

func (fw *FileWriter) open() error {
	path := filepath.Join(fw.dir, fw.name+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat log: %w", err)
	}

	fw.file = f
	fw.writer = bufio.NewWriterSize(f, fileFlushBuf)
	fw.size = info.Size()
	return nil
}

// Write writes a line to the log file, rotating if needed.
func (fw *FileWriter) Write(line string) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.closed {
		return nil
	}

	data := []byte(line + "\n")
	if _, err := fw.writer.Write(data); err != nil {
		return err
	}
	fw.size += int64(len(data))

	// Flush periodically (we rely on the caller to call Flush explicitly too).
	if fw.size >= int64(maxFileSize) {
		if err := fw.rotate(); err != nil {
			return err
		}
	}

	return nil
}

// Flush flushes the buffered writer.
func (fw *FileWriter) Flush() error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.closed || fw.writer == nil {
		return nil
	}
	return fw.writer.Flush()
}

// Close flushes and closes the file writer.
func (fw *FileWriter) Close() error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	fw.closed = true
	if fw.writer != nil {
		fw.writer.Flush()
	}
	if fw.file != nil {
		return fw.file.Close()
	}
	return nil
}

func (fw *FileWriter) rotate() error {
	if fw.writer != nil {
		fw.writer.Flush()
	}
	if fw.file != nil {
		fw.file.Close()
	}

	path := filepath.Join(fw.dir, fw.name+".log")
	oldPath := path + ".1"

	// Remove old backup.
	os.Remove(oldPath)

	// Rotate current to .1.
	if err := os.Rename(path, oldPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rotate: %w", err)
	}

	fw.size = 0
	return fw.open()
}

// LineWriter splits a byte stream into lines and sends them to a callback.
// It caps lines at maxLineLen bytes.
type LineWriter struct {
	buf      []byte
	callback func(line string)
}

// NewLineWriter creates a line-splitting writer.
func NewLineWriter(callback func(line string)) *LineWriter {
	return &LineWriter{callback: callback}
}

// Write implements io.Writer.
func (lw *LineWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		idx := -1
		for i, b := range p {
			if b == '\n' {
				idx = i
				break
			}
		}

		if idx < 0 {
			// No newline found — buffer the data.
			remaining := maxLineLen - len(lw.buf)
			if remaining > 0 {
				add := min(len(p), remaining)
				lw.buf = append(lw.buf, p[:add]...)
			}
			break
		}

		// Found newline — emit the line.
		line := append(lw.buf, p[:idx]...)
		if len(line) > maxLineLen {
			line = line[:maxLineLen]
		}
		lw.callback(string(line))
		lw.buf = lw.buf[:0]
		p = p[idx+1:]
	}
	return n, nil
}

// CombinedLogWriter creates an io.Writer that writes to both a ring buffer and a file writer.
func CombinedLogWriter(ring *RingBuffer, file *FileWriter) io.Writer {
	return NewLineWriter(func(line string) {
		ring.Add(line)
		if file != nil {
			file.Write(line)
		}
	})
}
