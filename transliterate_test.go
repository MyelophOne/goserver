package goserver

import "testing"

func TestTransliterate(t *testing.T) {
	tests := map[string]string{
		"Привет, мир!":         "privet-mir",
		"Щука, ёлка и подъезд": "schuka-yolka-i-podezd",
		"Łódź déjà vu":         "lodz-deja-vu",
		"Αθήνα και κόσμος":     "athhna-kai-kosmos",
		"  Café -- 2026  ":     "cafe-2026",
		"rock & roll / 100%":   "rock-roll-100",
		"АБВ ΓΔΕ ŁÓDŹ":         "abv-gde-lodz",
	}

	for input, want := range tests {
		if got := Transliterate(input); got != want {
			t.Errorf("Transliterate(%q) = %q, want %q", input, got, want)
		}
	}
}
