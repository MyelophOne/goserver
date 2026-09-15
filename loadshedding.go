package goserver

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

type workLeaseKey struct{}

type workLease struct {
	refs    atomic.Int32
	release func()
}

func (l *workLease) done() {
	if l.refs.Add(-1) == 0 {
		l.release()
	}
}

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
			lease := &workLease{release: func() { <-shedderSemaphore }}
			lease.refs.Store(1)
			defer lease.done()
			r = r.WithContext(context.WithValue(r.Context(), workLeaseKey{}, lease))

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
