package goserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponseHeader(t *testing.T) {
	handler := ResponseHeader("X-Powered-By", "goserver")(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if actual := recorder.Header().Get("X-Powered-By"); actual != "goserver" {
		t.Fatalf("X-Powered-By = %q, want %q", actual, "goserver")
	}
}

func TestResponseHeaderWithEmptyName(t *testing.T) {
	called := false
	handler := ResponseHeader("", "ignored")(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Fatal("next handler was not called")
	}
	if _, exists := recorder.Header()[""]; exists {
		t.Fatal("empty response header was added")
	}
}
