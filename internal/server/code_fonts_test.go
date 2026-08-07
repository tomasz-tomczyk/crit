package server

import "testing"

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
}
