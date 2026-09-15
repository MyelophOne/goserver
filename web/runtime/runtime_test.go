package runtime

import (
	"encoding/json"
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
