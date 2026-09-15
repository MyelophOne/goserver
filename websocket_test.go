package goserver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerWebSocketConfiguration(t *testing.T) {
	s := NewServer("0")
	hub := NewWebSocketHub()
	s.SetWebSocketHub(hub)
	s.SetWebSocketAuthorizer(func(r *http.Request) error {
		if r.Header.Get("X-WS-Test") != "ok" {
			return errors.New("unauthorized")
		}
		return nil
	})
	if s.wsHub != hub || s.wsAuthorize == nil {
		t.Fatal("WebSocket hub configuration was not retained")
	}
	if err := s.wsAuthorize(httptest.NewRequest("GET", "http://example.test/ws", nil)); err == nil {
		t.Fatal("WebSocket authorizer was not invoked")
	}
}
