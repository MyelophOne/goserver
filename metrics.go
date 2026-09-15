package goserver

import (
	"net/http"
	"strings"
	"time"
)

type metricsWriter struct {
	http.ResponseWriter
	isStream *bool
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func newStatusWriter(w http.ResponseWriter) *statusWriter {
	return &statusWriter{ResponseWriter: w, status: http.StatusOK}
}

func (w *statusWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *statusWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (mw *metricsWriter) WriteHeader(statusCode int) {
	if mw.isStream != nil && isStreamResponse(mw) {
		*mw.isStream = true
	}
	mw.ResponseWriter.WriteHeader(statusCode)
}

func (mw *metricsWriter) Flush() {
	if flusher, ok := mw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (mw *metricsWriter) Unwrap() http.ResponseWriter {
	return mw.ResponseWriter
}

func isStreamingContentType(ct string) bool {
	return strings.Contains(ct, "text/event-stream") ||
		strings.Contains(ct, "application/x-ndjson")
}

func isStreamResponse(w http.ResponseWriter) bool {
	ct := w.Header().Get("Content-Type")
	cc := w.Header().Get("Cache-Control")
	conn := w.Header().Get("Connection")
	cto := w.Header().Get("X-Content-Type-Options")
	return isStreamingContentType(ct) ||
		(strings.Contains(cc, "no-cache") && strings.Contains(cc, "no-transform") &&
			strings.Contains(conn, "keep-alive") && cto == "nosniff")
}

func (s *Server) MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		isStream := false
		mw := &metricsWriter{ResponseWriter: w, isStream: &isStream}

		next.ServeHTTP(mw, r)

		duration := time.Since(start)
		reqID := GetRequestID(r.Context())

		if isStream || isStreamResponse(mw) {
			return
		}

		if duration > 100*time.Millisecond {
			s.Logger.Printf("[%v] Slow request: %s %s took %v", reqID, r.Method, r.URL.Path, duration)
		}
	})
}
