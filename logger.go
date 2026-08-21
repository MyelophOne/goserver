package goserver

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

var recorderPool = sync.Pool{
	New: func() any {
		return &statusRecorder{}
	},
}

func acquireStatusRecorder(w http.ResponseWriter) *statusRecorder {
	r := recorderPool.Get().(*statusRecorder)
	r.ResponseWriter = w
	r.status = http.StatusOK
	r.size = 0
	return r
}

func releaseStatusRecorder(r *statusRecorder) {
	if r != nil {
		r.ResponseWriter = nil
		recorderPool.Put(r)
	}
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.size += size
	return size, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (s *Server) LogRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/.well-known/") || strings.HasPrefix(r.URL.Path, "/healthz") ||
			r.URL.Path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		next.ServeHTTP(w, r)

		reqID := GetRequestID(r.Context())

		s.Logger.Printf("[%v] %s %s - %v",
			reqID,
			r.Method,
			r.URL.Path,
			time.Since(start),
		)
	})
}

func (s *Server) ProdAccessLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/.well-known/") ||
			strings.HasPrefix(r.URL.Path, "/healthz") ||
			r.URL.Path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		recorder := acquireStatusRecorder(w)
		defer releaseStatusRecorder(recorder)

		next.ServeHTTP(recorder, r)

		duration := time.Since(start)

		reqID := GetRequestID(r.Context())

		clientIP := GetRealIP(r)

		statusText := http.StatusText(recorder.status)

		logMessage := fmt.Sprintf("[%s] | %d %s | %v | %s | %s %s",
			reqID,
			recorder.status,
			statusText,
			duration,
			clientIP,
			r.Method,
			r.URL.Path,
		)

		switch {
		case recorder.status >= 500:
			s.Logger.Printf("ERROR: %s", logMessage)
		case recorder.status >= 400:
			s.Logger.Printf("WARN:  %s", logMessage)
		default:
			s.Logger.Printf("INFO:  %s", logMessage)
		}
	})
}
