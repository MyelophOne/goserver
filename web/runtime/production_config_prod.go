//go:build myelophone_prod

package runtime

import "io/fs"

// Production uses the mode embedded by the build, never the process environment.
func configureWebEnabled(*RuntimeConfig) error { return nil }

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
