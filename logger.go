package goserver

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (s *Server) LogRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/.well-known/") || strings.HasPrefix(r.URL.Path, "/healthz") ||
			r.URL.Path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		next.ServeHTTP(w, r)

		var after runtime.MemStats
		runtime.ReadMemStats(&after)

		allocDelta := int64(after.Alloc) - int64(before.Alloc)

		deltaStr := ""
		if allocDelta >= 0 {
			deltaStr = FormatBytes(uint64(allocDelta))
		} else {
			deltaStr = "-" + FormatBytes(uint64(-allocDelta))
		}

		reqID := GetRequestID(r.Context())

		s.Logger.Printf("[%v] %s %s - %v | ΔMem: %s | Alloc: %s | Sys: %s | NumGC: %d",
			reqID,
			r.Method,
			r.URL.Path,
			time.Since(start),
			deltaStr,
			FormatBytes(after.Alloc),
			FormatBytes(after.Sys),
			after.NumGC,
		)
	})
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

func (s *Server) ProdAccessLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/.well-known/") ||
			strings.HasPrefix(r.URL.Path, "/healthz") ||
			r.URL.Path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()

		recorder := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

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
