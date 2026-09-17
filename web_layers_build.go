//go:build !myelophone_prod

package goserver

import (
	"io/fs"
	"os"
	"path/filepath"
)

func sourceModulePath(path, modulePath string) string {
	if _, err := os.Stat(sourceFilePath(path)); err == nil {
		return modulePath
	}
	return goserverModulePath
}

func materializeClientLayers() (string, error) {
	root, err := webBuildTemp("layers-")
	if err != nil {
		return "", err
	}
	for _, source := range []string{systemStoresDir, systemClientPluginsDir, systemRuntimeDir, systemClientDir} {
		err = sourceWalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name()[0] == '.' {
					return fs.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".js" && ext != ".mjs" && ext != ".json" {
				return nil
			}
			if entry.Name()[0] == '.' {
				return nil
			}
			data, readErr := sourceReadFile(path)
			if readErr != nil {
				return readErr
			}
			target := filepath.Join(root, sourcePath(path))
			if mkdirErr := os.MkdirAll(filepath.Dir(target), 0o755); mkdirErr != nil {
				return mkdirErr
			}
			return os.WriteFile(target, data, 0o644)
		})
		if err != nil && !os.IsNotExist(err) {
			_ = os.RemoveAll(root)
			return "", err
		}
	}
	return root, nil
}
