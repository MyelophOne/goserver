package goserver

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"
)

//go:embed error.html
var errorHTML string

var (
	ErrCookieNotFound = errors.New("cookie not found")
	ErrSessionInvalid = errors.New("invalid or expired session cookie")
)

func (s *Server) loadTemplates() error {
	tmpl, err := template.New("error").Parse(errorHTML)
	if err != nil {
		return err
	}
	s.errorTmpl = tmpl
	return nil
}

type ErrorPageData struct {
	StatusText string
	Message    string
	RequestID  string
	StatusCode int
}

func (s *Server) RenderError(w http.ResponseWriter, r *http.Request, statusCode int, message string) {
	if statusCode >= 400 && statusCode < 500 {
		atomic.AddInt64(&s.stats.Errors4xx, 1)
	} else if statusCode >= 500 {
		atomic.AddInt64(&s.stats.Errors5xx, 1)
	}

	if r.Method != http.MethodGet {
		s.RenderErrorJSON(w, r, statusCode, message)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	statusText := http.StatusText(statusCode)
	reqID := GetRequestID(r.Context())

	if message != "" && message != statusText {
		s.Logger.Printf("[%s] Error Details: %s", reqID, message)
	}
	data := ErrorPageData{
		StatusCode: statusCode,
		StatusText: statusText,
		Message:    message,
		RequestID:  reqID,
	}
	var buf strings.Builder
	if err := s.errorTmpl.Execute(&buf, data); err != nil {
		http.Error(w, http.StatusText(statusCode), statusCode)
		return
	}
	html := strings.Join(strings.Fields(buf.String()), " ")
	_, _ = w.Write([]byte(html))
	if s.ErrorNotifier != nil && statusCode >= 500 {
		traceErr := NewError(fmt.Sprintf("HTTP %d %s - %s", statusCode, statusText, message))
		s.ErrorNotifier(statusCode, r, traceErr.Error())
	}
}

func (s *Server) RenderErrorJSON(w http.ResponseWriter, r *http.Request, statusCode int, message string) {
	if statusCode >= 400 && statusCode < 500 {
		atomic.AddInt64(&s.stats.Errors4xx, 1)
	} else if statusCode >= 500 {
		atomic.AddInt64(&s.stats.Errors5xx, 1)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	statusText := http.StatusText(statusCode)
	reqID := GetRequestID(r.Context())

	if message != "" && message != statusText {
		s.Logger.Printf("[%s] Error Details: %s", reqID, message)
	}
	resp := struct {
		StatusText string `json:"statusText"`
		Message    string `json:"message,omitempty"`
		StatusCode int    `json:"statusCode"`
		RequestID  string `json:"requestID"`
	}{
		StatusCode: statusCode,
		StatusText: statusText,
		Message:    message,
		RequestID:  reqID,
	}
	_ = json.NewEncoder(w).Encode(resp)
	if s.ErrorNotifier != nil && statusCode >= 500 {
		traceErr := NewError(fmt.Sprintf("HTTP %d %s - %s", statusCode, statusText, message))
		s.ErrorNotifier(statusCode, r, traceErr.Error())
	}
}

func NewError(msg string) error {
	const depth = 32
	var pcs [depth]uintptr
	n := runtime.Callers(2, pcs[:])
	stack := pcs[:n]
	var sb strings.Builder
	sb.WriteString(msg)
	sb.WriteString("\n")
	frames := runtime.CallersFrames(stack)
	for {
		frame, more := frames.Next()
		if !more {
			break
		}
		if strings.Contains(frame.File, "runtime/") {
			continue
		}
		fmt.Fprintf(&sb, "  at %s:%d (%s)\n", frame.File, frame.Line, frame.Function)
	}
	return errors.New(sb.String())
}

func isIgnorableCloseError(err error) bool {
	if err == nil {
		return true
	}

	if errors.Is(err, net.ErrClosed) {
		return true
	}

	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "broken pipe")
}
