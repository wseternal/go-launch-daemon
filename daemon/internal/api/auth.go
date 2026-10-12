package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// authMiddleware wraps a handler with optional bearer token authentication.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token == "" {
			// Auth disabled — pass through.
			next(w, r)
			return
		}

		token := extractBearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}

		next(w, r)
	}
}

// extractBearerToken extracts the bearer token from the Authorization header
// or from the ?token= query parameter (for WebSocket upgrades).
func extractBearerToken(r *http.Request) string {
	// Check Authorization header first.
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// Fall back to query parameter (for WebSocket).
	return r.URL.Query().Get("token")
}
