package goserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

var ctxStreamKey = &struct{}{}

type SSEEvent struct {
	ID    string
	Event string
	Data  any
	Retry int
}

type ResponseStream struct {
	writer            http.ResponseWriter
	request           *http.Request
	flusher           http.Flusher
	ctx               context.Context
	arrayStarted      bool
	arrayFirstWritten bool
}

var streamBufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func acquireStreamBuf() *bytes.Buffer {
	b := streamBufPool.Get().(*bytes.Buffer)
	b.Reset()
	return b
}

func releaseStreamBuf(b *bytes.Buffer) {
	if b != nil {
		b.Reset()
		streamBufPool.Put(b)
	}
}

func (s *Server) NewStream(w http.ResponseWriter, r *http.Request, contentType string) (*ResponseStream, error) {
	return s.NewStreamWithStatus(w, r, contentType, http.StatusOK)
}

func (s *Server) NewStreamWithStatus(w http.ResponseWriter, r *http.Request, contentType string, statusCode int) (*ResponseStream, error) {
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	w.WriteHeader(statusCode)

	if dg, ok := w.(interface{ DisableGzip() }); ok {
		dg.DisableGzip()
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing (http.Flusher)")
	}

	flusher.Flush()

	ctx := context.WithValue(r.Context(), ctxStreamKey, true)

	return &ResponseStream{
		writer:  w,
		request: r.WithContext(ctx),
		flusher: flusher,
		ctx:     ctx,
	}, nil
}

func (s *Server) NewHTMLStream(w http.ResponseWriter, r *http.Request) (*ResponseStream, error) {
	return s.NewStream(w, r, "text/html; charset=utf-8")
}

func (s *Server) NewJSONStream(w http.ResponseWriter, r *http.Request) (*ResponseStream, error) {
	return s.NewStream(w, r, "application/json; charset=utf-8")
}

func (s *Server) NewNDJSONStream(w http.ResponseWriter, r *http.Request) (*ResponseStream, error) {
	return s.NewStream(w, r, "application/x-ndjson; charset=utf-8")
}

func (s *Server) NewSSEStream(w http.ResponseWriter, r *http.Request) (*ResponseStream, error) {
	return s.NewStream(w, r, "text/event-stream; charset=utf-8")
}

func (s *ResponseStream) Context() context.Context {
	return s.ctx
}

func (s *ResponseStream) IsClosed() bool {
	select {
	case <-s.ctx.Done():
		return true
	default:
		return false
	}
}

func (s *ResponseStream) Write(p []byte) (n int, err error) {
	if s.IsClosed() {
		return 0, s.ctx.Err()
	}
	n, err = s.writer.Write(p)
	if err == nil {
		s.flusher.Flush()
	}
	return n, err
}

func (s *ResponseStream) Flush() {
	s.flusher.Flush()
}

func (s *ResponseStream) WriteHTML(html string) (int, error) {
	return s.Write([]byte(html))
}

func (s *ResponseStream) WriteJSON(v any) error {
	if s.IsClosed() {
		return s.ctx.Err()
	}
	buf := acquireStreamBuf()
	defer releaseStreamBuf(buf)

	if err := json.NewEncoder(buf).Encode(v); err != nil {
		return err
	}
	if _, err := s.writer.Write(buf.Bytes()); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *ResponseStream) WriteNDJSON(v any) error {
	if s.IsClosed() {
		return s.ctx.Err()
	}
	buf := acquireStreamBuf()
	defer releaseStreamBuf(buf)

	jsonData, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal NDJSON: %w", err)
	}
	buf.Write(jsonData)
	buf.WriteByte('\n')

	if _, err := s.writer.Write(buf.Bytes()); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *ResponseStream) StartJSONArray() error {
	if _, err := s.Write([]byte("[")); err != nil {
		return err
	}
	s.arrayStarted = true
	s.arrayFirstWritten = false
	return nil
}

func (s *ResponseStream) WriteJSONArrayItem(v any) error {
	if s.IsClosed() {
		return s.ctx.Err()
	}

	buf := acquireStreamBuf()
	defer releaseStreamBuf(buf)

	jsonData, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON array item: %w", err)
	}

	if !s.arrayStarted {
		buf.WriteByte('[')
		s.arrayStarted = true
		s.arrayFirstWritten = true
	} else if !s.arrayFirstWritten {
		s.arrayFirstWritten = true
	} else {
		buf.WriteByte(',')
	}
	buf.Write(jsonData)

	if _, err := s.writer.Write(buf.Bytes()); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *ResponseStream) EndJSONArray() error {
	if !s.arrayStarted {
		_, err := s.Write([]byte("[]"))
		return err
	}
	_, err := s.Write([]byte("]"))
	s.arrayStarted = false
	s.arrayFirstWritten = false
	return err
}

func (s *ResponseStream) WriteSSEEvent(e SSEEvent) error {
	if s.IsClosed() {
		return s.ctx.Err()
	}

	buf := acquireStreamBuf()
	defer releaseStreamBuf(buf)

	if e.ID != "" {
		fmt.Fprintf(buf, "id: %s\n", e.ID)
	}
	if e.Event != "" {
		fmt.Fprintf(buf, "event: %s\n", e.Event)
	}
	if e.Retry > 0 {
		fmt.Fprintf(buf, "retry: %d\n", e.Retry)
	}

	if e.Data != nil {
		switch v := e.Data.(type) {
		case string:
			lines := strings.Split(v, "\n")
			for _, line := range lines {
				fmt.Fprintf(buf, "data: %s\n", line)
			}
		case []byte:
			lines := strings.Split(string(v), "\n")
			for _, line := range lines {
				fmt.Fprintf(buf, "data: %s\n", line)
			}
		default:
			jsonData, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("failed to marshal SSE data to JSON: %w", err)
			}
			fmt.Fprintf(buf, "data: %s\n", string(jsonData))
		}
	}

	buf.WriteByte('\n')

	if _, err := s.writer.Write(buf.Bytes()); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
