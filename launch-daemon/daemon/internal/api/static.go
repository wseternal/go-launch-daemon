package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// staticHandler serves static files from a directory with SPA fallback.
// API and agent routes are NOT affected — they remain JSON 404.
func staticHandler(staticDir string) http.Handler {
	if staticDir == "" {
		return http.NotFoundHandler()
	}

	// Verify the directory exists.
	info, err := os.Stat(staticDir)
	if err != nil || !info.IsDir() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// API-only mode — return JSON 404 for non-API routes.
			if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/agent/") {
				http.NotFound(w, r)
				return
			}
			http.NotFound(w, r)
		})
	}

	fileServer := http.FileServer(http.Dir(staticDir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// API and agent routes stay as-is (handled elsewhere).
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/agent/") {
			http.NotFound(w, r)
			return
		}

		// Try to serve the file directly.
		filePath := filepath.Join(staticDir, path)
		if _, err := os.Stat(filePath); err == nil {
			// File exists — serve it with appropriate cache headers.
			if path == "/index.html" || path == "/" {
				w.Header().Set("Cache-Control", "no-store")
			} else if strings.Contains(path, "/assets/") {
				// Vite hashed assets can be cached forever.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for non-API GET requests.
		if r.Method == http.MethodGet {
			indexPath := filepath.Join(staticDir, "index.html")
			if _, err := os.Stat(indexPath); err == nil {
				w.Header().Set("Cache-Control", "no-store")
				http.ServeFile(w, r, indexPath)
				return
			}
		}

		http.NotFound(w, r)
	})
}
