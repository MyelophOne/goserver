package goserver

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
)

const (
	minGzipSize = 1400
)

var gzipPool = sync.Pool{
	New: func() any {
		gz, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
		return gz
	},
}

var sniffBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, minGzipSize)
		return &b
	},
}

func acquireSniffBuf() []byte {
	b := sniffBufPool.Get().(*[]byte)
	buf := (*b)[:0]
	sniffBufPool.Put(b)
	return buf
}

func releaseSniffBuf(b []byte) {
	if cap(b) >= minGzipSize {
		bp := sniffBufPool.Get().(*[]byte)
		*bp = b[:0]
		sniffBufPool.Put(bp)
	}
}

func ResponseHeader(name, value string) Middleware {
	return func(next http.Handler) http.Handler {
		if name == "" {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(name, value)
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			strings.Contains(r.Header.Get("Connection"), "Upgrade") {
			next.ServeHTTP(w, r)
			return
		}

		grw := &gzipResponseWriter{
			ResponseWriter: w,
			sniffBuf:       acquireSniffBuf(),
			status:         http.StatusOK,
		}

		defer func() {
			_ = grw.Close()
			releaseSniffBuf(grw.sniffBuf)
		}()

		next.ServeHTTP(grw, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer      *gzip.Writer
	sniffBuf    []byte
	status      int
	wroteHeader bool
	headerSent  bool
	skipGzip    bool
}

func (w *gzipResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.status = statusCode
	w.wroteHeader = true
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.status = http.StatusOK
		w.wroteHeader = true
	}

	if len(b) == 0 {
		return 0, nil
	}

	if w.writer != nil {
		return w.writer.Write(b)
	}

	if w.skipGzip {
		return w.ResponseWriter.Write(b)
	}

	w.sniffBuf = append(w.sniffBuf, b...)

	if len(w.sniffBuf) >= minGzipSize {
		return w.flushBuffer(false)
	}

	return len(b), nil
}

func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *gzipResponseWriter) DisableGzip() {
	if w.skipGzip || w.writer != nil {
		return
	}
	w.skipGzip = true
	if !w.headerSent {
		if !w.wroteHeader {
			w.status = http.StatusOK
			w.wroteHeader = true
		}
		w.ResponseWriter.WriteHeader(w.status)
		w.headerSent = true
	}
}

func (w *gzipResponseWriter) flushBuffer(isClosing bool) (int, error) {
	ct := w.ResponseWriter.Header().Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(w.sniffBuf)
		w.ResponseWriter.Header().Set("Content-Type", ct)
	}

	ce := w.ResponseWriter.Header().Get("Content-Encoding")
	shouldGzip := !strings.Contains(ce, "gzip") &&
		!strings.Contains(ct, "image/") &&
		!strings.Contains(ct, "zip") &&
		!strings.Contains(ct, "pdf") &&
		!strings.Contains(ct, "video/")

	if isClosing && len(w.sniffBuf) < minGzipSize {
		shouldGzip = false
	}

	if shouldGzip {
		w.ResponseWriter.Header().Set("Content-Encoding", "gzip")
		w.ResponseWriter.Header().Del("Content-Length")

		if !w.headerSent {
			w.ResponseWriter.WriteHeader(w.status)
			w.headerSent = true
		}

		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(w.ResponseWriter)
		w.writer = gz

		n, err := w.writer.Write(w.sniffBuf)

		w.sniffBuf = nil
		return n, err
	}

	w.skipGzip = true

	if !w.headerSent {
		w.ResponseWriter.WriteHeader(w.status)
		w.headerSent = true
	}

	n, err := w.ResponseWriter.Write(w.sniffBuf)
	w.sniffBuf = nil
	return n, err
}

func (w *gzipResponseWriter) Close() error {
	if w.writer != nil {
		err := w.writer.Close()
		gzipPool.Put(w.writer)
		w.writer = nil
		return err
	}

	if w.skipGzip {
		return nil
	}

	if len(w.sniffBuf) > 0 {
		_, err := w.flushBuffer(true)
		return err
	}

	if !w.headerSent {
		w.ResponseWriter.WriteHeader(w.status)
		w.headerSent = true
	}

	return nil
}

func (w *gzipResponseWriter) Flush() {
	if w.writer == nil && !w.skipGzip && len(w.sniffBuf) > 0 {
		_, _ = w.flushBuffer(false)
	}
	if w.writer != nil {
		_ = w.writer.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (s *Server) RespondJSON(w http.ResponseWriter, r *http.Request, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.Logger.Printf("json encoding failed: %v", err)
	}
}

func (s *Server) RespondRawJSON(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, err := w.Write(data); err != nil {
		s.Logger.Printf("failed to write response: %v", err)
	}
}

func (s *Server) RespondHTML(w http.ResponseWriter, html string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write([]byte(html)); err != nil {
		s.Logger.Printf("failed to write response: %v", err)
	}
}

func (s *Server) RespondText(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write([]byte(text)); err != nil {
		s.Logger.Printf("failed to write response: %v", err)
	}
}

func (s *Server) RespondAsItIs(w http.ResponseWriter, text string) {
	if _, err := w.Write([]byte(text)); err != nil {
		s.Logger.Printf("failed to write response: %v", err)
	}
}

func (s *Server) RespondSecureJSON(w http.ResponseWriter, r *http.Request, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, err := w.Write([]byte("while(1);")); err != nil {
		s.Logger.Printf("failed to write response: %v", err)
	}
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.Logger.Printf("failed to write response: %v", err)
	}
}

func (s *Server) RespondXML(w http.ResponseWriter, r *http.Request, data any) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if err := xml.NewEncoder(w).Encode(data); err != nil {
		s.Logger.Printf("failed to write response: %v", err)
	}
}

func (s *Server) RespondAsciiJSON(w http.ResponseWriter, r *http.Request, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(data); err != nil {
		s.RenderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := w.Write(toASCII(buf.Bytes())); err != nil {
		s.Logger.Printf("failed to write response: %v", err)
	}
}

func (s *Server) WithGzip(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.GzipMiddleware(handler).ServeHTTP(w, r)
	}
}

func (s *Server) ServeFile(w http.ResponseWriter, r *http.Request, filePath string) {
	http.ServeFile(w, r, filePath)
}

func (s *Server) DownloadFile(w http.ResponseWriter, r *http.Request, filePath string, customFileName string) {
	if customFileName == "" {
		customFileName = filepath.Base(filePath)
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, customFileName))

	http.ServeFile(w, r, filePath)
}
