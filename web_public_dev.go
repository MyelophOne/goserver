//go:build !myelophone_prod

package goserver

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var publicModuleDirs sync.Map

func (modulePublicFS) filesystem() (fs.FS, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	key := root + "\x00" + os.Getenv("GOWORK") + "\x00" + os.Getenv("GOFLAGS")
	if dir, ok := publicModuleDirs.Load(key); ok {
		return os.DirFS(filepath.Join(dir.(string), "assets")), nil
	}
	output, err := exec.Command("go", "list", "-m", "-json", goserverModulePath).Output()
	if err != nil {
		return nil, err
	}
	var module struct{ Dir string }
	if err := json.Unmarshal(output, &module); err != nil {
		return nil, err
	}
	if module.Dir == "" {
		return nil, fs.ErrNotExist
	}
	publicModuleDirs.Store(key, module.Dir)
	return os.DirFS(filepath.Join(module.Dir, "assets")), nil
}

func publicAssetsFS(root string) fs.FS { return layeredPublicAssetsFS(root) }

type modulePublicFS struct{}

func (f modulePublicFS) Open(name string) (fs.File, error) {
	assets, err := f.filesystem()
	if err != nil {
		return nil, err
	}
	return assets.Open(name)
}

func (f modulePublicFS) ReadDir(name string) ([]fs.DirEntry, error) {
	assets, err := f.filesystem()
	if err != nil {
		return nil, err
	}
	return fs.ReadDir(assets, name)
}

func layeredPublicAssetsFS(root string) fs.FS {
	local := os.DirFS(root)
	if _, production := sourceFS(); production {
		return local
	}
	project := publicLayerFS{base: modulePublicFS{}, local: local}
	return publicLayerFS{base: project, local: playgroundPublicFS{root: root}}
}

type playgroundPublicFS struct{ root string }

func (f playgroundPublicFS) filesystem() (fs.FS, error) {
	if !playgroundEnabled.Load() {
		return nil, fs.ErrNotExist
	}
	return os.DirFS(filepath.Join(filepath.Dir(f.root), playgroundDir, "assets")), nil
}

func (f playgroundPublicFS) Open(name string) (fs.File, error) {
	assets, err := f.filesystem()
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return assets.Open(name)
}

func (f playgroundPublicFS) ReadDir(name string) ([]fs.DirEntry, error) {
	assets, err := f.filesystem()
	if err != nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
	}
	return fs.ReadDir(assets, name)
}

func generatedSiteSearchWorker(cleanPath string) (string, bool) {
	switch cleanPath {
	case "site-search-worker.js", "site-search-server-worker.js":
		return filepath.Join("tmp", "site-search", cleanPath), true
	default:
		return "", false
	}
}
