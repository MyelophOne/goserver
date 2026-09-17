//go:build !myelophone_prod

package goserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStaticAssetsMiddlewareServesInheritedFiles(t *testing.T) {
	t.Chdir(publicConsumerProject(t))
	s := NewServer("")
	handler := s.StaticAssetsMiddleware(http.NotFoundHandler())
	for _, url := range []string{"/assets/myelophone_eng.png", "/assets/myelophone_eng_white.png", "/assets/seo/goserver-cat.jpg", "/favicon.ico", "/icon.svg", "/apple-touch-icon.png"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			request := httptest.NewRequest(method, url, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Errorf("%s %s: status %d", method, url, response.Code)
			}
			if method == http.MethodGet && response.Body.Len() == 0 {
				t.Errorf("%s: empty asset", url)
			}
			if request.URL.Path != url {
				t.Errorf("request URL mutated: %s", request.URL.Path)
			}
		}
	}
	if err := os.MkdirAll("assets", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("assets", "icon.svg"), []byte("consumer-icon"), 0o644); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/assets/icon.svg", nil))
	if response.Code != http.StatusOK || response.Body.String() != "consumer-icon" {
		t.Fatalf("consumer override: status %d, body %q", response.Code, response.Body.String())
	}
	if err := os.Remove(filepath.Join("assets", "icon.svg")); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/icon.svg", nil))
	if response.Code != http.StatusOK || response.Body.String() == "consumer-icon" {
		t.Fatalf("restored base asset: status %d", response.Code)
	}
}
