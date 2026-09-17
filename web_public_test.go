//go:build !myelophone_prod

package goserver

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	logic "github.com/myelophone/goserver/web/runtime"
)

func TestPublicAssetsInheritAndOverride(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "assets")
	if err := os.MkdirAll(filepath.Join(local, "seo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "icon.svg"), []byte("consumer-icon"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "seo", "custom.svg"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "dist", "assets")
	if err := copyPublicOptimized(local, destination, logic.RuntimeConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"favicon.ico", "apple-touch-icon.png", "myelophone_eng.png", "seo/goserver-cat.jpg", "seo/custom.svg"} {
		if _, err := os.Stat(filepath.Join(destination, filepath.FromSlash(name))); err != nil {
			t.Errorf("missing asset %s: %v", name, err)
		}
	}
	data, err := fs.ReadFile(publicAssetsFS(local), "icon.svg")
	if err != nil || string(data) != "consumer-icon" {
		t.Fatalf("local override: %q, %v", data, err)
	}
	data, err = os.ReadFile(filepath.Join(destination, "icon.svg"))
	if err != nil || string(data) != "consumer-icon" {
		t.Fatalf("production override: %q, %v", data, err)
	}
}

func publicConsumerProject(t *testing.T) string {
	t.Helper()
	frameworkRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	mod := fmt.Sprintf("module example.com/assets\n\ngo 1.27.1\n\nrequire github.com/myelophone/goserver v0.0.0\n\nreplace github.com/myelophone/goserver => %s\n", goQuote(filepath.ToSlash(frameworkRoot)))
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPublicAssetsDiskLayerAndOptionalRobots(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	if err := os.MkdirAll(filepath.Join(base, "assets", "downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(base, "go.mod"), "module github.com/myelophone/goserver\n\ngo 1.27.1\n")
	write(filepath.Join(base, "assets", "downloads", "custom.bin"), "disk-base")
	write(filepath.Join(root, "go.mod"), fmt.Sprintf("module example.com/assets\n\ngo 1.27.1\n\nrequire github.com/myelophone/goserver v0.0.0\n\nreplace github.com/myelophone/goserver => %s\n", goQuote(filepath.ToSlash(base))))
	t.Chdir(root)
	if err := copyPublicOptimized("assets", "dist/assets", logic.RuntimeConfig{}, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("dist/assets/downloads/custom.bin")
	if err != nil || string(data) != "disk-base" {
		t.Fatalf("arbitrary inherited disk asset: %q, %v", data, err)
	}
	embedPath, err := writeProductionEmbed(nil, logic.ProductionBundle{})
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(embedPath)
	if err != nil || strings.Contains(string(data), "assets/robots.txt") || strings.Contains(string(data), "custom.bin") {
		t.Fatalf("unexpected embedded assets: %v", err)
	}
	if err := os.MkdirAll("assets", 0o755); err != nil {
		t.Fatal(err)
	}
	write("assets/robots.txt", "User-agent: *\nDisallow: /private\n")
	embedPath, err = writeProductionEmbed(nil, logic.ProductionBundle{})
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(embedPath)
	if err != nil || !strings.Contains(string(data), "all:production/sources") {
		t.Fatalf("missing optional embedded robots: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(generatedWebDir, "production", "sources", "assets", "robots.txt"))
	if err != nil || string(data) != "User-agent: *\nDisallow: /private\n" {
		t.Fatalf("prepared robots content: %q, %v", data, err)
	}
}
