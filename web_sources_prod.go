//go:build myelophone_prod

package goserver

import "io/fs"

func configureSourceEnvironment(string) {}

func sourceReadFile(name string) ([]byte, error) {
	sources, ok := sourceFS()
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return fs.ReadFile(sources, sourcePath(name))
}

func sourceReadDir(name string) ([]fs.DirEntry, error) {
	sources, ok := sourceFS()
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return fs.ReadDir(sources, sourcePath(name))
}

func sourceWalkDir(root string, fn fs.WalkDirFunc) error {
	sources, ok := sourceFS()
	if !ok {
		return fn(root, nil, &fs.PathError{Op: "walk", Path: root, Err: fs.ErrNotExist})
	}
	return fs.WalkDir(sources, sourcePath(root), fn)
}
