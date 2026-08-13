package goserver

import (
	"net/http"
	"strings"
	"sync"
)

var (
	shedderSemaphore chan struct{}
	shedderOnce      sync.Once
)

func (s *Server) LoadSheddingMiddleware(next http.Handler) http.Handler {
	shedderOnce.Do(func() {
		limit := s.Config.maxConcurrent
		if limit <= 0 {
			limit = 1000
		}
		shedderSemaphore = make(chan struct{}, limit)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/debug/pprof/") {
			next.ServeHTTP(w, r)
			return
		}

		select {
		case shedderSemaphore <- struct{}{}:
			defer func() { <-shedderSemaphore }()

			next.ServeHTTP(w, r)

		default:
			reqID := GetRequestID(r.Context())

			s.Logger.Printf("[%s] LOAD SHEDDING: Rejecting request %s %s (Max %d concurrent connections reached)",
				reqID, r.Method, r.URL.Path, cap(shedderSemaphore))

			w.Header().Set("Retry-After", "5")
			w.Header().Set("X-RateLimit-Error", "Server Overload")

			if s.ResponseMode == "json" {
				s.RenderErrorJSON(w, r, http.StatusServiceUnavailable, "Server is currently overloaded. Please try again later.")
			} else {
				s.RenderError(w, r, http.StatusServiceUnavailable, "Server is currently overloaded. Please try again later.")
			}
		}
	})
}
