package goserver

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogRequestIncludesResponseStatus(t *testing.T) {
	var output bytes.Buffer
	server := NewServer("0")
	server.SetLogger(log.New(&output, "", 0))
	handler := server.LogRequest(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://example.test/demo", nil))
	if !strings.Contains(output.String(), "GET 404 /demo -") {
		t.Fatalf("access log must include method, response status and path: %q", output.String())
	}
}

func TestProdAccessLoggerOnlyLogsClientIPWhenExplicitlyEnabled(t *testing.T) {
	var output bytes.Buffer
	server := NewServer("0")
	server.SetLogger(log.New(&output, "", 0))
	handler := server.ProdAccessLogger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "http://example.test/demo", nil)
	request.RemoteAddr = "198.51.100.42:12345"
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if strings.Contains(output.String(), "198.51.100.42") || strings.Contains(output.String(), "client=") {
		t.Fatalf("client IP must be absent by default: %q", output.String())
	}

	output.Reset()
	server.Config.LogClientIP = "masked"
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if strings.Contains(output.String(), "198.51.100.42") || !strings.Contains(output.String(), "client=198.51.100.x") {
		t.Fatalf("client IP must be masked: %q", output.String())
	}

	output.Reset()
	server.Config.LogClientIP = "full"
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if !strings.Contains(output.String(), "client=198.51.100.42") {
		t.Fatalf("explicit client IP logging was not applied: %q", output.String())
	}
}
