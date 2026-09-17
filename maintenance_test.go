package goserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaintenanceModeReturnsTemporaryPage(t *testing.T) {
	s := NewServer("")
	handler := s.buildHandler(Config{
		MaintenanceMode: true,
		MaxURLLength:    2048,
		MaxHeaders:      100,
	})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/any-route", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if got := response.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("Retry-After"); got != "300" {
		t.Fatalf("Retry-After = %q, want 300", got)
	}
	if !strings.Contains(response.Body.String(), "The site is being updated") {
		t.Fatal("maintenance page is missing its update message")
	}
}

func TestMaintenanceModeDoesNotWriteBodyForHead(t *testing.T) {
	response := httptest.NewRecorder()
	serveMaintenance(response, httptest.NewRequest(http.MethodHead, "/", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("HEAD response body length = %d, want 0", response.Body.Len())
	}
}

func TestMaintenanceModeRefreshesFromSiblingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance")
	mode := newMaintenanceModeAt(Config{MaintenanceCheckInterval: time.Nanosecond}, path)
	if mode.enabled() {
		t.Fatal("missing maintenance file must disable maintenance mode")
	}
	if err := os.WriteFile(path, []byte("enabled"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !mode.enabled() {
		t.Fatal("maintenance file must enable maintenance mode")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if mode.enabled() {
		t.Fatal("removing maintenance file must disable maintenance mode")
	}
}

func TestMaintenanceModeBypassToken(t *testing.T) {
	s := NewServer("")
	s.GET("/check", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := s.buildHandler(Config{
		MaintenanceMode:        true,
		maintenanceBypassToken: "test-token",
		MaxURLLength:           2048,
		MaxHeaders:             100,
	})

	request := httptest.NewRequest(http.MethodGet, "/check", nil)
	request.Header.Set(maintenanceBypassHeader, "test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d for a bypass request", response.Code, http.StatusNoContent)
	}
}
