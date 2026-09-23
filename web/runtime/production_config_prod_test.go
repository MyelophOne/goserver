//go:build myelophone_prod

package runtime

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"testing"
)

func TestProductionWebModeIgnoresEnvironment(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, value := range []string{"true", "false", "invalid", ""} {
			t.Run(fmt.Sprintf("enabled=%t/env=%s", enabled, value), func(t *testing.T) {
				t.Setenv("MYELOPHONE_WEB_ENABLED", value)
				RegisterProductionConfig(map[string][]byte{
					"websettings.json": []byte(fmt.Sprintf(`{"runtime":{"enabled":%t}}`, enabled)),
				})
				configOnce, configErr = sync.Once{}, nil
				t.Cleanup(func() {
					RegisterProductionConfig(nil)
					configOnce, configErr = sync.Once{}, nil
				})
				cfg, err := UseRuntimeConfig()
				if err != nil || cfg.Runtime.Enabled != enabled {
					t.Fatalf("enabled = %t, error = %v; want %t", cfg.Runtime.Enabled, err, enabled)
				}
			})
		}
	}
}

func TestProductionConfigurationIgnoresDevelopmentFiles(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	if environment() != "Production" {
		t.Fatal("production build selected development environment")
	}
	t.Chdir(t.TempDir())
	if err := os.WriteFile("websettings.Development.json", []byte(`{"runtime":{"enabled":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := configFile("websettings.Development.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("production read development settings: %v", err)
	}
}
