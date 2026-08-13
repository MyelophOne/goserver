package goserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

type timeoutWriter struct {
	w           http.ResponseWriter
	mu          sync.Mutex
	timedOut    bool
	wroteHeader bool
	code        int
}

func (tw *timeoutWriter) Header() http.Header {
	return tw.w.Header()
}

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}

	tw.wroteHeader = true
	return tw.w.Write(p)
}

func (tw *timeoutWriter) WriteHeader(statusCode int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.timedOut || tw.wroteHeader {
		return
	}

	tw.wroteHeader = true
	tw.code = statusCode
	tw.w.WriteHeader(statusCode)
}

func (s *Server) TimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/debug/pprof/") {
				next.ServeHTTP(w, r)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			r = r.WithContext(ctx)

			tw := &timeoutWriter{w: w}

			done := make(chan struct{})
			panicChan := make(chan any, 1)

			go func() {
				defer func() {
					if p := recover(); p != nil {
						panicChan <- p
					}
				}()

				next.ServeHTTP(tw, r)
				close(done)
			}()

			select {
			case p := <-panicChan:
				tw.mu.Lock()
				defer tw.mu.Unlock()
				if !tw.timedOut {
					panic(p)
				}

			case <-done:

			case <-ctx.Done():
				tw.mu.Lock()
				defer tw.mu.Unlock()

				tw.timedOut = true

				if !tw.wroteHeader {
					code := http.StatusGatewayTimeout
					msg := http.StatusText(code)

					if errors.Is(ctx.Err(), context.Canceled) {
						code = 499
						msg = "Client Closed Request"
					}

					if s.ResponseMode == "json" {
						s.RenderErrorJSON(w, r, code, msg)
					} else {
						s.RenderError(w, r, code, msg)
					}
				}
			}
		})
	}
}
