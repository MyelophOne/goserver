//go:build !myelophone_prod

package goserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	logic "github.com/myelophone/goserver/web/runtime"
)

func TestProcessFinalCSS(t *testing.T) {
	if _, err := resolveNodeBinary(); err != nil {
		t.Skipf("Node.js is required for the configured PostCSS pipeline: %v", err)
	}
	if _, err := os.Stat(filepath.Join(systemTailwindDir, "node_modules", "postcss", "package.json")); err != nil {
		t.Skip("run `task setup:tailwind` to install PostCSS before exercising the final CSS pipeline")
	}
	css, err := processFinalCSS(`
:root { --remove-me: ; --keep-me: blue; }
.h-screen { height: 100vh; }
.sample { min-height: 100dvh; width: 25dvw; }
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--keep-me: blue",
		"height: 100vh; height: 100dvh",
		"min-height: 100vh; min-height: 100dvh",
		"width: 25vw; width: 25dvw",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("final CSS misses %q: %s", want, css)
		}
	}
	if strings.Contains(css, "--remove-me") {
		t.Fatalf("empty custom property was not removed: %s", css)
	}
	app := &App{styles: map[string]string{}, finalCSS: map[string]string{}}
	href := app.registerStyle(`.tenant { height: 40dvh; --empty: ; }`)
	if got := app.styles[strings.TrimSuffix(strings.TrimPrefix(href, "/_gosh/style/"), ".css")]; !strings.Contains(got, "height: 40vh; height: 40dvh") || strings.Contains(got, "--empty") {
		t.Fatalf("registered CSS did not pass the final pipeline: %q", got)
	}
}

func TestUnusedPublicAssetsUsesStaticReferences(t *testing.T) {
	const usedName = "audit-test-used.svg"
	const unusedName = "audit-test-unused.svg"
	usedPath := filepath.Join(systemPublicDir, usedName)
	unusedPath := filepath.Join(systemPublicDir, unusedName)
	sourcePath := filepath.Join(systemComponentsDir, "AuditAssetReference.gosh")
	if err := os.WriteFile(usedPath, []byte("used"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unusedPath, []byte("unused"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte(`<img src="/assets/audit-test-used.svg"><img src="/audit-test-used.svg">`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(usedPath)
		_ = os.Remove(unusedPath)
		_ = os.Remove(sourcePath)
	})

	files := unusedPublicAssets()
	for _, file := range files {
		if file.Path == "assets/"+usedName {
			t.Fatalf("referenced asset was reported: %#v", files)
		}
		if file.Path == "assets/"+unusedName && file.Kind == "asset" {
			return
		}
	}
	t.Fatalf("unreferenced asset was not reported: %#v", files)
}

func TestUnusedPublicAssetsKeepsFaviconsAndConfiguredFiles(t *testing.T) {
	var cfg logic.RuntimeConfig
	cfg.Build.IncludeFiles = []string{"downloads/catalog.pdf"}
	for _, name := range []string{"favicon.ico", "favicon.svg", "favicon.png", "downloads/catalog.pdf"} {
		if !alwaysIncludePublicAsset("assets/"+name, cfg) {
			t.Fatalf("delivery asset %q was not protected", name)
		}
	}
}

func TestWriteEmptyUnusedReportRemovesReport(t *testing.T) {
	original, err := os.ReadFile(unusedReportPath)
	existed := err == nil
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.MkdirAll(filepath.Dir(unusedReportPath), 0o755)
			_ = os.WriteFile(unusedReportPath, original, 0o644)
		}
	})
	if err := os.MkdirAll(filepath.Dir(unusedReportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unusedReportPath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteUnusedReport(UnusedReport{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unusedReportPath); !os.IsNotExist(err) {
		t.Fatalf("empty report exists: %v", err)
	}
}
