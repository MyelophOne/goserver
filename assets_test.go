//go:build !myelophone_prod

package goserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestStaticAssetsMiddlewareServesRootPublicFiles(t *testing.T) {
	want, err := os.ReadFile("assets/icon.svg")
	if err != nil {
		t.Fatal(err)
	}

	s := NewServer("")
	handler := s.StaticAssetsMiddleware(http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodGet, "/icon.svg", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("root public file status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.String() != string(want) {
		t.Fatalf("root public file body does not match assets/icon.svg")
	}
}
