//go:build !myelophone_prod

package goserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebLayersInheritPagesAndPreferConsumerFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	pages, err := LoadPages("web/pages")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := pages.Match("/"); !ok || pages.NotFound == nil || pages.ErrorPage == nil {
		t.Fatal("base pages were not inherited")
	}
	if err := os.MkdirAll("web/pages", 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"index.gosh":  "<!-- @layout none -->\n<template>consumer-index</template>",
		"custom.gosh": "<!-- @layout none -->\n<template>consumer-custom</template>",
	} {
		if err := os.WriteFile(filepath.Join("web/pages", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pages, err = LoadPages("web/pages")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := pages.Match("/custom"); !ok {
		t.Fatal("consumer page missing")
	}
	data, err := sourceReadFile("web/pages/index.gosh")
	if err != nil || !strings.Contains(string(data), "consumer-index") {
		t.Fatalf("consumer page did not override base: %s, %v", data, err)
	}
	if err := os.Remove("web/pages/index.gosh"); err != nil {
		t.Fatal(err)
	}
	data, err = sourceReadFile("web/pages/index.gosh")
	if err != nil || !strings.Contains(string(data), "MyelophoneWelcome") {
		t.Fatalf("base page did not return: %s, %v", data, err)
	}
	css, err := loadDefaultCSS()
	if err != nil || !strings.Contains(css, "--") {
		t.Fatalf("base CSS missing: %v", err)
	}
	if err := os.WriteFile("go.mod", []byte("module example.com/consumer\n\ngo 1.27.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := GenerateWebLogicBindings(); err != nil {
		t.Fatal(err)
	}
	bindings, err := os.ReadFile(filepath.Join(generatedWebDir, "bindings_gen.go"))
	if err != nil || !strings.Contains(string(bindings), "github.com/myelophone/goserver/web/system/logic") {
		t.Fatalf("base Go handlers missing: %s, %v", bindings, err)
	}
	if _, err := os.Stat("web/system"); !os.IsNotExist(err) {
		t.Fatalf("generation created web/system: %v", err)
	}
	entry, err := os.ReadFile("cmd/web_import_gen.go")
	if err != nil || !strings.Contains(string(entry), "example.com/consumer/internal/goservergen") {
		t.Fatalf("automatic generated import missing: %s, %v", entry, err)
	}
}

func TestWebPlaygroundOverridesProjectOnlyInDevelopment(t *testing.T) {
	previous := playgroundEnabled.Load()
	t.Cleanup(func() { playgroundEnabled.Store(previous) })
	t.Chdir(t.TempDir())
	for path, source := range map[string]string{
		"web/pages/index.gosh":               "<template>project-index</template>",
		"web/playground/pages/index.gosh":    "<template>playground-index</template>",
		"web/playground/pages/dev-only.gosh": "<template>playground-only</template>",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, environment := range []string{"Development", "Production"} {
		configureSourceEnvironment(environment)
		data, err := sourceReadFile("web/pages/index.gosh")
		want := "project-index"
		if environment == "Development" {
			want = "playground-index"
		}
		if err != nil || !strings.Contains(string(data), want) {
			t.Fatalf("%s page: %s, %v", environment, data, err)
		}
		pages, err := LoadPages("web/pages")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, exists := pages.Match("/dev-only"); exists != (environment == "Development") {
			t.Fatalf("%s playground page discovered: %v", environment, exists)
		}
	}
}
