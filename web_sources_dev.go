//go:build !myelophone_prod

package goserver

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

const playgroundDir = "./web/playground"

var playgroundEnabled atomic.Bool

func configureSourceEnvironment(environment string) {
	playgroundEnabled.Store(strings.EqualFold(environment, "Development"))
}

func playgroundPath(name string) (string, bool) {
	if _, production := sourceFS(); production || !playgroundEnabled.Load() {
		return "", false
	}
	webRoot, err := filepath.Abs("web")
	if err != nil {
		return "", false
	}
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(webRoot, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	if relative == "tenants" || strings.HasPrefix(relative, "tenants"+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(playgroundDir, relative), true
}

func sourceFilePath(name string) string {
	if candidate, ok := playgroundPath(name); ok {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return name
}

func sourceReadFile(name string) ([]byte, error) {
	if f, ok := sourceFS(); ok {
		return fs.ReadFile(f, sourcePath(name))
	}
	if filepath.IsAbs(filepath.FromSlash(sourcePath(name))) {
		return os.ReadFile(sourceFilePath(name))
	}
	return fs.ReadFile(webSourceFS(), sourcePath(name))
}

func sourceReadDir(name string) ([]fs.DirEntry, error) {
	if f, ok := sourceFS(); ok {
		return fs.ReadDir(f, sourcePath(name))
	}
	if filepath.IsAbs(filepath.FromSlash(sourcePath(name))) {
		return os.ReadDir(name)
	}
	return fs.ReadDir(webSourceFS(), sourcePath(name))
}

func sourceWalkDir(root string, fn fs.WalkDirFunc) error {
	if f, ok := sourceFS(); ok {
		return fs.WalkDir(f, sourcePath(root), fn)
	}
	virtualRoot := sourcePath(root)
	if filepath.IsAbs(filepath.FromSlash(virtualRoot)) {
		return filepath.WalkDir(root, fn)
	}
	return fs.WalkDir(webSourceFS(), virtualRoot, func(path string, entry fs.DirEntry, err error) error {
		relative, relErr := filepath.Rel(filepath.FromSlash(virtualRoot), filepath.FromSlash(path))
		if relErr != nil {
			return relErr
		}
		return fn(filepath.Join(root, relative), entry, err)
	})
}
