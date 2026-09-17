//go:build myelophone_prod

package goserver

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestProductionDoesNotReadDevelopmentSources(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll("web/pages", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("web/pages/index.gosh", []byte("development-only"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceReadFile("web/pages/index.gosh"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("production read development source: %v", err)
	}
	if err := os.MkdirAll("tmp/site-search", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("tmp/site-search", "site-search-worker.js"), []byte("development-only"), 0644); err != nil {
		t.Fatal(err)
	}
	if HasPublicFile("/site-search-worker.js") {
		t.Fatal("production serves temporary development worker")
	}
}
