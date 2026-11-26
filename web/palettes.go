package webassets

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/tomasz-tomczyk/crit/internal/config"
)

const palettesJSPrefix = "window.crit=window.crit||{};window.crit.palettes="

type shippedPaletteEntry struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type shippedPaletteSets struct {
	light map[string]bool
	dark  map[string]bool
}

var loadShippedPalettes = sync.OnceValues(func() (shippedPaletteSets, error) {
	compressed, err := FS.ReadFile("pierre/palettes.js.gz")
	if err != nil {
		return shippedPaletteSets{}, fmt.Errorf("reading embedded palettes: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return shippedPaletteSets{}, fmt.Errorf("decompressing embedded palettes: %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return shippedPaletteSets{}, fmt.Errorf("reading decompressed palettes: %w", err)
	}
	trimmed := bytes.TrimSpace(raw)
	if !bytes.HasPrefix(trimmed, []byte(palettesJSPrefix)) {
		return shippedPaletteSets{}, fmt.Errorf("unexpected embedded palettes.js format")
	}
	payload := bytes.TrimPrefix(trimmed, []byte(palettesJSPrefix))
	payload = bytes.TrimSuffix(payload, []byte(";"))

	var entries []shippedPaletteEntry
	if err := json.Unmarshal(payload, &entries); err != nil {
		return shippedPaletteSets{}, fmt.Errorf("parsing embedded palettes: %w", err)
	}
	light := make(map[string]bool)
	dark := make(map[string]bool)
	for _, entry := range entries {
		switch entry.Type {
		case "light":
			light[entry.ID] = true
		case "dark":
			dark[entry.ID] = true
		}
	}
	return shippedPaletteSets{light: light, dark: dark}, nil
})

// ShippedPalettes returns the sets of embedded light and dark palette IDs from
// web/pierre/palettes.js.gz.
func ShippedPalettes() (light, dark map[string]bool, err error) {
	sets, err := loadShippedPalettes()
	if err != nil {
		return nil, nil, err
	}
	return sets.light, sets.dark, nil
}

// ValidateSettingsPalettes checks lightPalette and darkPalette entries in
// settings against the shipped palettes in web/pierre/palettes.js.gz, printing
// a warning and removing any invalid or wrong-mode entry.
func ValidateSettingsPalettes(settings map[string]any, source string) {
	if len(settings) == 0 {
		return
	}
	light, dark, err := ShippedPalettes()
	if err != nil {
		return
	}
	config.FilterSettingsPalettes(settings, light, dark, source)
}
