package goserver

import (
	"net/http"
	"time"
)

func (s *Server) MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		reqID := GetRequestID(r.Context())

		if duration > 100*time.Millisecond {
			s.Logger.Printf("[%v] Slow request: %s %s took %v", reqID, r.Method, r.URL.Path, duration)
		}
	})
}
