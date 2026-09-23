//go:build !myelophone_prod

package runtime

import (
	"os"
	"strconv"
	"strings"
)

func configureWebEnabled(config *RuntimeConfig) error {
	if value, ok := os.LookupEnv("MYELOPHONE_WEB_ENABLED"); ok {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		config.Runtime.Enabled = enabled
	}
	return nil
}

func configFile(path string) ([]byte, error) {
	productionState.RLock()
	data, ok := productionState.config[path]
	productionState.RUnlock()
	if ok {
		return append([]byte(nil), data...), nil
	}
	return os.ReadFile(path)
}

func environment() string {
	value := strings.TrimSpace(os.Getenv("APP_ENV"))
	if value == "" {
		value = strings.TrimSpace(os.Getenv("MYELOPHONE_ENV"))
	}
	if strings.EqualFold(value, "prod") || strings.EqualFold(value, "production") {
		return "Production"
	}
	return "Development"
}
