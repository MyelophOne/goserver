package goserver

import (
	"strings"
	"testing"
	"time"

	logic "github.com/myelophone/goserver/web/runtime"
)

func TestConfigStringOmitsUnsetValuesAndRedactsSecrets(t *testing.T) {
	got := (Config{
		APIPrefix:        "/api",
		PostgresPassword: "password",
	}).String()

	if !strings.Contains(got, "APIPrefix:/api") {
		t.Fatalf("configured field is missing: %q", got)
	}
	if strings.Contains(got, "DatabaseUrl:") {
		t.Fatalf("unset field is present: %q", got)
	}
	if strings.Contains(got, "password") || !strings.Contains(got, "PostgresPassword:[REDACTED]") {
		t.Fatalf("secret was not redacted: %q", got)
	}
}

func TestConfigStringFormatsDurationsForHumans(t *testing.T) {
	got := (Config{
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 500 * time.Millisecond,
		MaxHeaderBytes:    64 << 10,
		MaxBodySize:       1 << 20,
	}).String()

	if !strings.Contains(got, "ReadTimeout:15s") || !strings.Contains(got, "ReadHeaderTimeout:500ms") {
		t.Fatalf("durations must be human-readable: %q", got)
	}
	if strings.Contains(got, "ReadTimeout:15000000000") || strings.Contains(got, "ReadHeaderTimeout:500000000") {
		t.Fatalf("durations must not be formatted as nanoseconds: %q", got)
	}
	if !strings.Contains(got, "MaxHeaderBytes:64KiB") || !strings.Contains(got, "MaxBodySize:1MiB") {
		t.Fatalf("byte limits must be human-readable: %q", got)
	}
}

func TestLoadConfigReadsMaintenanceMode(t *testing.T) {
	t.Setenv("MAINTENANCE_MODE", "true")
	t.Setenv("MAINTENANCE_CHECK_INTERVAL", "10s")
	t.Setenv("MAINTENANCE_BYPASS_TOKEN", "test-token")
	s := NewServer("")

	if !s.Config.MaintenanceMode {
		t.Fatal("MAINTENANCE_MODE=true must enable maintenance mode")
	}
	if s.Config.MaintenanceCheckInterval != 10*time.Second {
		t.Fatalf("maintenance check interval = %s, want 10s", s.Config.MaintenanceCheckInterval)
	}
	if s.Config.maintenanceBypassToken != "test-token" {
		t.Fatal("MAINTENANCE_BYPASS_TOKEN was not loaded")
	}
}

func TestFormatConfiguredValuesOmitsEmptyWebSettings(t *testing.T) {
	var cfg logic.RuntimeConfig
	cfg.Render.DefaultLayout = "default"

	got := formatConfiguredValues(cfg)
	if !strings.Contains(got, "Render:{DefaultLayout:default}") {
		t.Fatalf("configured web values are missing: %q", got)
	}
	if strings.Contains(got, "Content:") || strings.Contains(got, "RouteRules:") || strings.Contains(got, "Server:") {
		t.Fatalf("empty web settings are present: %q", got)
	}
}

func TestStartupConfigCombinesGoserverAndWebSettings(t *testing.T) {
	s := &Server{Config: Config{APIPrefix: "/api"}, web: &App{}}
	s.web.config.Render.DefaultLayout = "default"

	got := s.startupConfig()
	for _, expected := range []string{"Goserver:{APIPrefix:/api}", "Web:{Render:{DefaultLayout:default}}"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("combined startup config is missing %q: %q", expected, got)
		}
	}
	if strings.Contains(got, "Content:") || strings.Contains(got, "RouteRules:") {
		t.Fatalf("combined startup config includes empty values: %q", got)
	}
}
