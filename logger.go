package goserver

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type LogLevel uint8

const (
	LogDebug LogLevel = iota
	LogInfo
	LogWarn
	LogError
	LogFatal
	LogOff
)

func ParseLogLevel(value string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return LogDebug
	case "warn", "warning":
		return LogWarn
	case "error":
		return LogError
	case "fatal":
		return LogFatal
	case "off", "none", "silent":
		return LogOff
	default:
		return LogInfo
	}
}

type ClientIPLogMode uint8

const (
	LogClientIPOff ClientIPLogMode = iota
	LogClientIPMasked
	LogClientIPFull
)

func ParseClientIPLogMode(value string) ClientIPLogMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "false", "none":
		return LogClientIPOff
	case "full", "true":
		return LogClientIPFull
	default:
		return LogClientIPMasked
	}
}

func (mode ClientIPLogMode) String() string {
	switch mode {
	case LogClientIPOff:
		return "off"
	case LogClientIPFull:
		return "full"
	default:
		return "masked"
	}
}

func (level LogLevel) String() string {
	switch level {
	case LogDebug:
		return "debug"
	case LogWarn:
		return "warn"
	case LogError:
		return "error"
	case LogFatal:
		return "fatal"
	case LogOff:
		return "off"
	default:
		return "info"
	}
}

type levelWriter struct {
	Target  io.Writer
	Minimum LogLevel
}

func (w levelWriter) Write(p []byte) (int, error) {
	if logMessageLevel(p) < w.Minimum {
		return len(p), nil
	}
	if w.Target == nil {
		return len(p), nil
	}
	_, err := w.Target.Write(p)
	return len(p), err
}

func logMessageLevel(message []byte) LogLevel {
	text := strings.ToUpper(string(message))
	switch {
	case strings.Contains(text, "FATAL:"), strings.Contains(text, "CRITICAL:"), strings.Contains(text, "PANIC"):
		return LogFatal
	case strings.Contains(text, "ERROR:"), strings.Contains(text, " ERROR "), strings.Contains(text, " ERROR:"), strings.Contains(text, "FAILED"):
		return LogError
	case strings.Contains(text, "WARN:"), strings.Contains(text, "WARNING:"), strings.Contains(text, " REJECTED"), strings.Contains(text, " BLOCKED"):
		return LogWarn
	case strings.Contains(text, "DEBUG:"):
		return LogDebug
	default:
		return LogInfo
	}
}

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
		recorder := newStatusWriter(w)
		next.ServeHTTP(recorder, r)

		reqID := GetRequestID(r.Context())

		s.Logger.Printf("[%v] %s %d %s - %v",
			reqID,
			r.Method,
			recorder.status,
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

		statusText := http.StatusText(recorder.status)
		logMessage := fmt.Sprintf("[%s] | %d %s | %v | %s %s",
			reqID, recorder.status, statusText, duration, r.Method, r.URL.Path)
		if mode := ParseClientIPLogMode(s.Config.LogClientIP); mode != LogClientIPOff {
			logMessage = fmt.Sprintf("[%s] | %d %s | %v | client=%s | %s %s",
				reqID, recorder.status, statusText, duration, formatLogClient(GetRealIP(r), mode), r.Method, r.URL.Path)
		}

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

func formatLogClient(address string, mode ClientIPLogMode) string {
	if address == "::1" || address == "127.0.0.1" {
		return "localhost"
	}
	if mode == LogClientIPMasked {
		return maskLogClientIP(address)
	}
	return address
}

func maskLogClientIP(address string) string {
	ip := net.ParseIP(address)
	if ip == nil {
		return "masked"
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return fmt.Sprintf("%d.%d.%d.x", ipv4[0], ipv4[1], ipv4[2])
	}
	ipv6 := ip.To16()
	if ipv6 == nil {
		return "masked"
	}
	for i := 6; i < len(ipv6); i++ {
		ipv6[i] = 0
	}
	return ipv6.String()
}
