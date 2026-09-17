//go:build !myelophone_prod

package goserver

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	logic "github.com/myelophone/goserver/web/runtime"
)

func TestPublicAssetLayersAndDevelopmentPlayground(t *testing.T) {
	previous := playgroundEnabled.Load()
	t.Cleanup(func() { playgroundEnabled.Store(previous) })
	configureSourceEnvironment("Production")
	t.Chdir(publicConsumerProject(t))
	base, err := fs.ReadFile(publicAssetsFS("assets"), "icon.svg")
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		"assets/icon.svg":                    "project-icon",
		"web/playground/assets/icon.svg":     "playground-icon",
		"web/playground/assets/dev-only.txt": "playground-only",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := NewServer("0")
	handler := s.StaticAssetsMiddleware(http.NotFoundHandler())
	check := func(want string) {
		t.Helper()
		for _, path := range []string{"/icon.svg", "/assets/icon.svg"} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK || response.Body.String() != want {
				t.Fatalf("%s: status %d, body %q, want %q", path, response.Code, response.Body.String(), want)
			}
		}
	}
	check("project-icon")
	configureSourceEnvironment("Development")
	check("playground-icon")
	if !HasPublicFile("/dev-only.txt") {
		t.Fatal("development playground addition missing")
	}
	configureSourceEnvironment("Production")
	check("project-icon")
	if HasPublicFile("/dev-only.txt") {
		t.Fatal("production source mode includes playground asset")
	}
	if err := copyPublicOptimized("assets", "dist/assets", logic.RuntimeConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	compiled, err := os.ReadFile("dist/assets/icon.svg")
	if err != nil || string(compiled) != "project-icon" {
		t.Fatalf("compiled project override: %q, %v", compiled, err)
	}
	if _, err := os.Stat("dist/assets/dev-only.txt"); !os.IsNotExist(err) {
		t.Fatalf("playground asset entered distribution: %v", err)
	}
	configureSourceEnvironment("Development")
	if err := os.Remove("web/playground/assets/icon.svg"); err != nil {
		t.Fatal(err)
	}
	check("project-icon")
	if err := os.Remove("assets/icon.svg"); err != nil {
		t.Fatal(err)
	}
	check(string(base))
}
