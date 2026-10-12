package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// gzipWriter wraps http.ResponseWriter with gzip compression.
type gzipWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w *gzipWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

// gzipPool reuses gzip writers to reduce allocation.
var gzipPool = sync.Pool{
	New: func() any {
		gz, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return gz
	},
}

// gzipMiddleware compresses responses for clients that accept gzip.
// Skips WebSocket upgrades and hijacked connections.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip if client doesn't accept gzip.
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// Skip WebSocket upgrades.
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}

		// Skip hijacked connections.
		if r.Header.Get("Connection") == "Upgrade" {
			next.ServeHTTP(w, r)
			return
		}

		gz := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gz)
		gz.Reset(w)
		defer gz.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length") // Length unknown after compression.

		next.ServeHTTP(&gzipWriter{Writer: gz, ResponseWriter: w}, r)
	})
}
