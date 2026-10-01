package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCustomWebSettingsOverrideDefaults(t *testing.T) {
	defaults, err := json.Marshal(defaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	var merged map[string]any
	if err := json.Unmarshal(defaults, &merged); err != nil {
		t.Fatal(err)
	}
	var custom map[string]any
	if err := json.Unmarshal([]byte(`{"seo":{"image":"/assets/seo/custom-cover.png"}}`), &custom); err != nil {
		t.Fatal(err)
	}
	mergeObject(merged, custom)
	data, err := json.Marshal(merged)
	if err != nil {
		t.Fatal(err)
	}
	var config RuntimeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.SEO.Image != "/assets/seo/custom-cover.png" {
		t.Fatalf("SEO image = %q", config.SEO.Image)
	}
}

func TestSSRStreamSettingsDefaultAndRouteOverrides(t *testing.T) {
	config := defaultConfig()
	if config.SSRStream {
		t.Fatal("SSR streaming must be disabled by default")
	}
	if err := json.Unmarshal([]byte(`{
		"ssrStream":true,
		"routeRules":{
			"/account":{"ssrStream":false},
			"/catalog/**":{"ssrStream":true}
		}
	}`), &config); err != nil {
		t.Fatal(err)
	}
	if !config.SSRStream {
		t.Fatal("global SSR streaming setting was not decoded")
	}
	if override := config.RouteRules["/account"].SSRStream; override == nil || *override {
		t.Fatalf("disabled route override = %v", override)
	}
	if override := config.RouteRules["/catalog/**"].SSRStream; override == nil || !*override {
		t.Fatalf("enabled route override = %v", override)
	}
}

func TestPlaygroundWebSettingsOnlyApplyInDevelopment(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join("web", "playground")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "websettings.json"), []byte(`{"ssrStream":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	development := defaultConfig()
	development.Environment = "Development"
	if err := mergePlaygroundConfig(&development); err != nil {
		t.Fatal(err)
	}
	if !development.SSRStream {
		t.Fatal("development did not load playground websettings")
	}

	production := defaultConfig()
	production.Environment = "Production"
	if err := mergePlaygroundConfig(&production); err != nil {
		t.Fatal(err)
	}
	if production.SSRStream {
		t.Fatal("production loaded playground websettings")
	}
}
