//go:build myelophone_prod

package goserver

import (
	"io/fs"
	"os"
	"path/filepath"
)

func publicAssetsFS(root string) fs.FS { return productionPublicFS{root: root} }

type productionPublicFS struct{ root string }

func (f productionPublicFS) filesystem() (fs.FS, error) {
	if filepath.IsAbs(f.root) {
		return os.DirFS(f.root), nil
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return os.DirFS(filepath.Join(filepath.Dir(executable), f.root)), nil
}

func (f productionPublicFS) Open(name string) (fs.File, error) {
	assets, err := f.filesystem()
	if err != nil {
		return nil, err
	}
	return assets.Open(name)
}

func (f productionPublicFS) ReadDir(name string) ([]fs.DirEntry, error) {
	assets, err := f.filesystem()
	if err != nil {
		return nil, err
	}
	return fs.ReadDir(assets, name)
}

func generatedSiteSearchWorker(string) (string, bool) { return "", false }
