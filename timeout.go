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
	header      http.Header
	mu          sync.Mutex
	timedOut    bool
	wroteHeader bool
	code        int
}

func (tw *timeoutWriter) Header() http.Header {
	return tw.header
}

func newTimeoutWriter(w http.ResponseWriter) *timeoutWriter {
	return &timeoutWriter{
		w:      w,
		header: w.Header().Clone(),
	}
}

func (tw *timeoutWriter) syncHeader() {
	dst := tw.w.Header()
	for key := range dst {
		delete(dst, key)
	}
	for key, values := range tw.header {
		dst[key] = append([]string(nil), values...)
	}
}

func (tw *timeoutWriter) Write(p []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}

	tw.syncHeader()
	tw.wroteHeader = true
	return tw.w.Write(p)
}

func (tw *timeoutWriter) WriteHeader(statusCode int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if statusCode >= 100 && statusCode < 200 {
		if tw.timedOut {
			return
		}
		tw.syncHeader()
		tw.w.WriteHeader(statusCode)
		return
	}

	if tw.timedOut || tw.wroteHeader {
		return
	}

	tw.wroteHeader = true
	tw.code = statusCode
	tw.syncHeader()
	tw.w.WriteHeader(statusCode)
}

func (tw *timeoutWriter) Flush() {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	if tw.timedOut {
		return
	}

	tw.syncHeader()
	tw.wroteHeader = true
	if flusher, ok := tw.w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (tw *timeoutWriter) Unwrap() http.ResponseWriter {
	return tw.w
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

			tw := newTimeoutWriter(w)

			done := make(chan struct{})
			panicChan := make(chan any, 1)
			if !s.beginWork() {
				http.Error(w, "Server shutting down", http.StatusServiceUnavailable)
				return
			}
			lease, _ := r.Context().Value(workLeaseKey{}).(*workLease)
			if lease != nil {
				lease.refs.Add(1)
			}

			go func() {
				defer s.backgroundWg.Done()
				if lease != nil {
					defer lease.done()
				}
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
