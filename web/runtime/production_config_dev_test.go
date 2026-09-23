//go:build !myelophone_prod

package runtime

import (
	"os"
	"sync"
	"testing"
)

func TestDevelopmentWebModeSelection(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name, settings, env string
		want, wantErr       bool
	}{
		{"default", `{}`, "", false, false},
		{"settings", `{"runtime":{"enabled":true}}`, "", true, false},
		{"enable", `{}`, "true", true, false},
		{"disable", `{"runtime":{"enabled":true}}`, "false", false, false},
		{"invalid", `{}`, "invalid", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MYELOPHONE_WEB_ENABLED", tc.env)
			if tc.env == "" {
				if err := os.Unsetenv("MYELOPHONE_WEB_ENABLED"); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile("websettings.json", []byte(tc.settings), 0o644); err != nil {
				t.Fatal(err)
			}
			configOnce, configErr = sync.Once{}, nil
			t.Cleanup(func() { configOnce, configErr = sync.Once{}, nil })
			cfg, err := UseRuntimeConfig()
			if (err != nil) != tc.wantErr || cfg.Runtime.Enabled != tc.want {
				t.Fatalf("enabled = %t, error = %v; want %t, error %t", cfg.Runtime.Enabled, err, tc.want, tc.wantErr)
			}
		})
	}
}
