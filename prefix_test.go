package goserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeAPIPrefix(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":        "",
		"/":       "",
		"api":     "/api",
		"/api":    "/api",
		"/api/":   "/api",
		"api/v1/": "/api/v1",
	}

	for input, expected := range tests {
		if actual := normalizeAPIPrefix(input); actual != expected {
			t.Errorf("normalizeAPIPrefix(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestDefaultsApplyAPIPrefixOnce(t *testing.T) {
	s := NewServer(":0")
	s.Config.APIPrefix = "/api/"
	s.Defaults()
	s.GET("/users", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "http://localhost/api/users", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	recorder := httptest.NewRecorder()

	s.buildHandler(s.Config).ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("prefixed route status = %d, want %d; body=%q", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
}

func TestSMTPWorkersConfiguration(t *testing.T) {
	t.Setenv("SMTP_FROM", "sender@example.com")
	t.Setenv("SMTP_WORKERS", "7")

	s := NewServer(":0")

	if s.Config.SmtpWorkers != "7" {
		t.Fatalf("SmtpWorkers = %q, want %q", s.Config.SmtpWorkers, "7")
	}
}
