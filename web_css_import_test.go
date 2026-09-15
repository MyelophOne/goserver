package goserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOptionalCSSExpandsLocalImports(t *testing.T) {
	dir := filepath.Join(systemWebCSSDir, "css-import-test")
	entry := filepath.Join(dir, "entry.css")
	imported := filepath.Join(dir, "parts", "button.css")
	if err := os.MkdirAll(filepath.Dir(imported), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte(`@import "./parts/button.css";`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imported, []byte(`.button { color: rebeccapurple; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	css, err := loadOptionalCSS(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(css, ".button { color: rebeccapurple; }") || strings.Contains(css, "@import") {
		t.Fatalf("local import was not expanded: %q", css)
	}
}

func TestLoadOptionalCSSRejectsImportOutsideEntryDirectory(t *testing.T) {
	dir := filepath.Join(systemWebCSSDir, "css-import-test-outside")
	entry := filepath.Join(dir, "entry.css")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte(`@import "../default.css";`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	if _, err := loadOptionalCSS(entry); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("outside import error = %v, want containment error", err)
	}
}

func TestCombineDefaultCSSKeepsProjectLayerLast(t *testing.T) {
	css := combineDefaultCSS(`.button { color: blue; }`, `.button { color: red; }`)
	if strings.Index(css, "blue") >= strings.Index(css, "red") {
		t.Fatalf("project default CSS must follow system CSS: %q", css)
	}
}

func TestSystemDefaultCSSKeepsFrameworkLoaderRulesAfterImports(t *testing.T) {
	css, err := loadOptionalCSS(filepath.Join(systemFrameworkCSSDir, "default.css"))
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{
		".js #gosh-preloader",
		".js #gosh-preloader.is-loading",
		"gosh-component[data-gosh-lazy] > .gosh-system-loader",
	} {
		if !strings.Contains(css, selector) {
			t.Fatalf("imported system CSS is missing %q", selector)
		}
	}
}

func TestGridColumnUsesPredictableNuxtCompatibleSpans(t *testing.T) {
	css, err := os.ReadFile(filepath.Join(systemComponentsDir, "grid", "Col.gosh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		`[data-grid-col][data-span="1"] { flex-basis: 8.333333%; }`,
		`[data-grid-col][data-span="4"] { flex-basis: 33.333333%; }`,
		`[data-grid-col][data-md-span="4"] { flex-basis: 33.333333%; }`,
		`[data-grid-col][data-span="12"] { flex-basis: 100%; }`,
	} {
		if !strings.Contains(string(css), rule) {
			t.Fatalf("grid span is missing %q", rule)
		}
	}
	if strings.Contains(string(css), "var(--grid-gap") {
		t.Fatal("GridCol must not bake Row gaps into span widths")
	}
}
