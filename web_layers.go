//go:build !myelophone_prod

package goserver

import (
	"io/fs"
	"os"
	"path/filepath"
)

func webSourceFS() fs.FS {
	var sources fs.FS = publicLayerFS{base: embeddedWeb, local: os.DirFS(".")}
	if playgroundEnabled.Load() {
		sources = publicLayerFS{base: sources, local: playgroundSourceFS{}}
	}
	return sources
}

type playgroundSourceFS struct{}

func (playgroundSourceFS) Open(name string) (fs.File, error) {
	path, ok := playgroundPath(filepath.FromSlash(name))
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return os.Open(path)
}

func (playgroundSourceFS) ReadDir(name string) ([]fs.DirEntry, error) {
	path, ok := playgroundPath(filepath.FromSlash(name))
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return os.ReadDir(path)
}
