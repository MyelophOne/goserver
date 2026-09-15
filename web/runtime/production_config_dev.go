//go:build !myelophone_prod

package runtime

import "os"

func configFile(path string) ([]byte, error) { return os.ReadFile(path) }
