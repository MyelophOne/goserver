package goserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	logic "github.com/myelophone/goserver/web/runtime"
)

func TestProductionRuntimeConfigEmbedsResolvedSettings(t *testing.T) {
	cfg := logic.RuntimeConfig{}
	cfg.Render.ServerTiming = true
	path, err := writeProductionRuntimeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `\"serverTiming\":true`) {
		t.Fatalf("resolved Server-Timing setting was not embedded: %s", data)
	}
}

func TestReachableProductionFilesIncludesTeleportDependencies(t *testing.T) {
	components, err := LoadComponents(systemComponentsDir)
	if err != nil {
		t.Fatal(err)
	}
	teleports, err := LoadComponents(systemTeleportDir)
	if err != nil {
		t.Fatal(err)
	}
	var roots []*Component
	for _, name := range teleports.Names() {
		teleport, _ := teleports.Get(name)
		roots = append(roots, teleport)
		components.items[normalizeComponentLookup(name)] = teleport
	}
	components.names = componentNames(components.items)
	pages, err := LoadPages(systemPagesDir)
	if err != nil {
		t.Fatal(err)
	}

	files := reachableProductionFiles(pages, components, nil, "", roots)
	contained := make(map[string]bool, len(files))
	for _, file := range files {
		contained[file] = true
	}
	for _, want := range []string{
		sourcePath(filepath.Join(systemComponentsDir, "ui", "CursorCreative.gosh")),
		sourcePath(filepath.Join(systemContentDir, "demo.md")),
	} {
		if !contained[want] {
			t.Fatalf("production files do not include %q", want)
		}
	}
}

func TestCopyContentResourcesPublishesOnlyCompanionFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "content")
	dst := filepath.Join(root, "dist", "assets", "content")
	if err := os.MkdirAll(filepath.Join(src, "guides", "intro"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "guides", "intro.md"), []byte("# Intro"), 0o644); err != nil {
		t.Fatal(err)
	}
	cover := []byte("svg cover")
	if err := os.WriteFile(filepath.Join(src, "guides", "intro", "cover.svg"), cover, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "guides", "intro", "notes.pdf"), []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "guides", "intro", ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	copied, err := copyContentResources(src, dst, logic.RuntimeConfig{})
	if err != nil {
		t.Fatalf("copyContentResources: %v", err)
	}
	if copied != 2 {
		t.Fatalf("copied = %d, want 2", copied)
	}
	if _, err := os.Stat(filepath.Join(dst, "guides", "intro.md")); !os.IsNotExist(err) {
		t.Fatalf("Markdown was published: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "guides", "intro", "cover.svg"))
	if err != nil || string(data) != string(cover) {
		t.Fatalf("cover was not copied unchanged: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "guides", "intro", "notes.pdf")); err != nil {
		t.Fatalf("non-image resource was not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "guides", "intro", ".gitkeep")); !os.IsNotExist(err) {
		t.Fatalf("hidden content file was published: %v", err)
	}
}

func TestCopyContentResourcesDoesNotCreateAnEmptyDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "content")
	dst := filepath.Join(root, "dist", "assets", "content")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "post.md"), []byte("# Post"), 0o644); err != nil {
		t.Fatal(err)
	}
	if copied, err := copyContentResources(src, dst, logic.RuntimeConfig{}); err != nil || copied != 0 {
		t.Fatalf("copyContentResources = (%d, %v), want (0, nil)", copied, err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("empty content destination exists: %v", err)
	}
}

func TestContentResourceURL(t *testing.T) {
	if got := contentResourceURL("/assets/content", "guides", "cover.jpg?size=1200"); got != "/assets/content/guides/cover.jpg?size=1200" {
		t.Fatalf("relative content resource URL = %q", got)
	}
	if got := contentResourceURL("/assets/tenants/acme/content", "posts", "../cover.jpg"); got != "/assets/tenants/acme/content/cover.jpg" {
		t.Fatalf("nested content resource URL = %q", got)
	}
	for _, reference := range []string{"/assets/seo/cover.jpg", "https://cdn.example/cover.jpg", "#image"} {
		if got := contentResourceURL("/assets/content", "guides", reference); got != reference {
			t.Fatalf("stable URL %q changed to %q", reference, got)
		}
	}
}

func TestContentResourcesAreServedFromSourceDuringDevelopment(t *testing.T) {
	path := filepath.Join(systemContentDir, "test-content-resource.txt")
	if err := os.WriteFile(path, []byte("content resource"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	app := &App{}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://example.test/assets/content/test-content-resource.txt", nil)
	request.URL.Path = "/content/test-content-resource.txt"
	if !app.serveContentResource(response, request) {
		t.Fatal("content resource was not handled")
	}
	if response.Code != http.StatusOK || response.Body.String() != "content resource" {
		t.Fatalf("content resource response: status=%d body=%q", response.Code, response.Body.String())
	}
}
