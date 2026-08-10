package server

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-text/typesetting/fontscan"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func TestEqualAdvances(t *testing.T) {
	tests := []struct {
		name     string
		advances []float32
		want     bool
	}{
		{name: "fixed ASCII advances", advances: []float32{500, 500, 500}, want: true},
		{name: "proportional advances", advances: []float32{500, 700}, want: false},
		{name: "missing glyph", advances: []float32{500, 0, 500}, want: false},
		{name: "empty", advances: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equalAdvances(tt.advances); got != tt.want {
				t.Errorf("equalAdvances(%v) = %v, want %v", tt.advances, got, tt.want)
			}
		})
	}
}

func TestAcceptsCodeFontRequiresASCIICoverageForFixedPitchFonts(t *testing.T) {
	valid := make([]float32, len(codeFontASCIIRunes))
	for i := range valid {
		valid[i] = 500
	}
	if !acceptsCodeFont(true, valid) {
		t.Fatal("fixed-pitch font with complete ASCII coverage was rejected")
	}
	if acceptsCodeFont(true, valid[:len(valid)-1]) {
		t.Fatal("fixed-pitch font without complete ASCII coverage was accepted")
	}
	valid[0] = 0
	if acceptsCodeFont(true, valid) {
		t.Fatal("fixed-pitch font with a zero-width ASCII glyph was accepted")
	}
	if allAdvancesNonZero(nil) {
		t.Fatal("empty advances were accepted")
	}
	if isCodeMonospace(nil) {
		t.Fatal("nil font was accepted")
	}
}

func TestCollectCodeFontFamiliesFiltersDeduplicatesAndSorts(t *testing.T) {
	footprints := []fontscan.Footprint{
		{Location: fontscan.Location{File: "zeta"}},
		{Location: fontscan.Location{File: "alpha"}},
		{Location: fontscan.Location{File: "duplicate"}},
		{Location: fontscan.Location{File: "proportional"}},
		{Location: fontscan.Location{File: "blank"}},
		{Location: fontscan.Location{File: "broken"}},
		{Location: fontscan.Location{File: "panics"}},
	}
	inspect := func(location fontscan.Location) (string, bool, error) {
		switch location.File {
		case "zeta":
			return "Zeta Mono", true, nil
		case "alpha", "duplicate":
			return " Alpha Mono ", true, nil
		case "proportional":
			return "Readable Sans", false, nil
		case "blank":
			return "  ", true, nil
		case "panics":
			panic("unsupported font")
		default:
			return "", false, errors.New("bad font")
		}
	}

	got := collectCodeFontFamilies(footprints, inspect)
	want := []string{"Alpha Mono", "Zeta Mono"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("families = %v, want %v", got, want)
	}
}

func TestDiscoverCodeFontFamiliesScansTheCurrentSystem(t *testing.T) {
	families, err := discoverCodeFontFamilies()
	if err != nil {
		t.Fatalf("discoverCodeFontFamilies() error = %v", err)
	}
	if families == nil {
		t.Fatal("discoverCodeFontFamilies() returned a nil result")
	}
}

func TestInspectCodeFontParsesMonoAndProportionalFonts(t *testing.T) {
	writeFont := func(name string, data []byte) fontscan.Location {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return fontscan.Location{File: path}
	}

	family, accepted, err := inspectCodeFont(writeFont("Go-Mono.ttf", gomono.TTF))
	if err != nil {
		t.Fatal(err)
	}
	if family != "Go Mono" || !accepted {
		t.Fatalf("Go Mono inspection = (%q, %v), want (%q, true)", family, accepted, "Go Mono")
	}

	_, accepted, err = inspectCodeFont(writeFont("Go-Regular.ttf", goregular.TTF))
	if err != nil {
		t.Fatal(err)
	}
	if accepted {
		t.Fatal("proportional Go Regular was accepted")
	}
}

func TestLoadCodeFontRejectsInvalidFileAndFaceIndex(t *testing.T) {
	if _, err := loadCodeFont(fontscan.Location{File: filepath.Join(t.TempDir(), "missing.ttf")}); err == nil {
		t.Fatal("missing font was loaded")
	}

	badPath := filepath.Join(t.TempDir(), "broken.ttf")
	if err := os.WriteFile(badPath, []byte("not a font"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCodeFont(fontscan.Location{File: badPath}); err == nil {
		t.Fatal("invalid font was loaded")
	}
	if _, accepted, err := inspectCodeFont(fontscan.Location{File: badPath}); err == nil || accepted {
		t.Fatalf("invalid font inspection = (accepted %v, error %v)", accepted, err)
	}

	monoPath := filepath.Join(t.TempDir(), "Go-Mono.ttf")
	if err := os.WriteFile(monoPath, gomono.TTF, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCodeFont(fontscan.Location{File: monoPath, Index: 1}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("out-of-range face error = %v, want os.ErrNotExist", err)
	}
}
