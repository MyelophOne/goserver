//go:build myelophone_prod

package runtime

import "io/fs"

func environment() string { return "Production" }

func configFile(path string) ([]byte, error) {
	productionState.RLock()
	data, ok := productionState.config[path]
	productionState.RUnlock()
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
	return append([]byte(nil), data...), nil
}
