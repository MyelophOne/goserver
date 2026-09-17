//go:build myelophone_prod

package runtime

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

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
