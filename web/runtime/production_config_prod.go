//go:build myelophone_prod

package runtime

import "os"

var productionConfigFiles map[string][]byte

func configFile(path string) ([]byte, error) {
	if data, ok := productionConfigFiles[path]; ok {
		return append([]byte(nil), data...), nil
	}
	return os.ReadFile(path)
}
