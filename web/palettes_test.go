package webassets

import "testing"

func TestShippedPalettes(t *testing.T) {
	light, dark, err := ShippedPalettes()
	if err != nil {
		t.Fatalf("ShippedPalettes failed: %v", err)
	}
	if len(light) == 0 {
		t.Fatal("expected shipped light palettes to be non-empty")
	}
	if len(dark) == 0 {
		t.Fatal("expected shipped dark palettes to be non-empty")
	}
	if !light["github-light"] {
		t.Errorf("expected github-light in light palettes")
	}
	if !dark["tokyo-night"] {
		t.Errorf("expected tokyo-night in dark palettes")
	}
	for id := range light {
		if dark[id] {
			t.Errorf("palette %q listed in both light and dark sets", id)
		}
	}
}

func TestValidateSettingsPalettes(t *testing.T) {
	settings := map[string]any{
		"lightPalette": "github-light",
		"darkPalette":  "tokyo-night",
	}
	ValidateSettingsPalettes(settings, "test-config")
	if settings["lightPalette"] != "github-light" || settings["darkPalette"] != "tokyo-night" {
		t.Fatalf("valid palettes unexpectedly dropped: %#v", settings)
	}

	bad := map[string]any{
		"lightPalette": "tokyo-night",
		"darkPalette":  "definitely-not-a-palette",
	}
	ValidateSettingsPalettes(bad, "test-config")
	if len(bad) != 0 {
		t.Fatalf("expected invalid palettes to be removed, got %#v", bad)
	}
}
