package server

import (
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/fontscan"
)

// codeFontASCIIRunes are deliberately limited to characters code reviewers
// commonly read. Some CJK programming fonts use a different width for CJK
// glyphs, so a whole-font fixed-pitch check rejects them despite their code
// characters lining up correctly.
var codeFontASCIIRunes = []rune{' ', '!', '0', 'A', 'W', 'a', 'i', 'm', '{', '}', '[', ']', '(', ')'}

// discoverCodeFontFamilies returns installed font families suitable for code.
// A malformed, unreadable, or unsupported font is simply ignored: the picker
// must remain useful even when a system font directory contains a bad file.
func discoverCodeFontFamilies() (families []string, err error) {
	// Font discovery and parsing operate on arbitrary system files. Keep an
	// upstream parser bug from taking down the HTTP request (or the daemon).
	defer func() {
		if recovered := recover(); recovered != nil {
			families = nil
			err = fmt.Errorf("discover system fonts: %v", recovered)
		}
	}()

	footprints, err := fontscan.SystemFonts(log.New(io.Discard, "", 0), "")
	if err != nil {
		return nil, err
	}
	return collectCodeFontFamilies(footprints, inspectCodeFont), nil
}

type codeFontInspector func(fontscan.Location) (family string, accepted bool, err error)

func collectCodeFontFamilies(footprints []fontscan.Footprint, inspect codeFontInspector) []string {
	accepted := make(map[string]string)
	for _, footprint := range footprints {
		family, suitable, err := safelyInspectCodeFont(inspect, footprint.Location)
		if err != nil {
			continue
		}
		if !suitable {
			continue
		}
		family = strings.TrimSpace(family)
		if family == "" {
			continue
		}
		key := strings.ToLower(family)
		if _, exists := accepted[key]; !exists {
			accepted[key] = family
		}
	}

	families := make([]string, 0, len(accepted))
	for _, family := range accepted {
		families = append(families, family)
	}
	sort.Slice(families, func(i, j int) bool {
		return strings.ToLower(families[i]) < strings.ToLower(families[j])
	})
	return families
}

func safelyInspectCodeFont(inspect codeFontInspector, location fontscan.Location) (family string, accepted bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			family = ""
			accepted = false
			err = fmt.Errorf("inspect code font: %v", recovered)
		}
	}()
	return inspect(location)
}

func inspectCodeFont(location fontscan.Location) (string, bool, error) {
	ft, err := loadCodeFont(location)
	if err != nil {
		return "", false, err
	}
	return ft.Describe().Family, isCodeMonospace(ft), nil
}

func loadCodeFont(location fontscan.Location) (*font.Font, error) {
	f, err := os.Open(location.File)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	loaders, err := ot.NewLoaders(f)
	if err != nil {
		return nil, err
	}
	if int(location.Index) >= len(loaders) {
		return nil, os.ErrNotExist
	}
	return font.NewFont(loaders[location.Index])
}

func isCodeMonospace(ft *font.Font) bool {
	if ft == nil {
		return false
	}
	advances, ok := codeFontASCIIAdvances(ft)
	return ok && acceptsCodeFont(ft.IsMonospace(), advances)
}

func codeFontASCIIAdvances(ft *font.Font) ([]float32, bool) {
	face := font.NewFace(ft)
	advances := make([]float32, 0, len(codeFontASCIIRunes))
	for _, r := range codeFontASCIIRunes {
		gid, ok := ft.NominalGlyph(r)
		if !ok {
			return nil, false
		}
		advance := face.HorizontalAdvance(gid)
		if advance <= 0 {
			return nil, false
		}
		advances = append(advances, advance)
	}
	return advances, true
}

// acceptsCodeFont requires representative ASCII coverage even when a font's
// metadata says it is fixed pitch. That keeps fixed-advance icon fonts out of
// a picker intended for source code.
func acceptsCodeFont(isMonospace bool, asciiAdvances []float32) bool {
	if len(asciiAdvances) != len(codeFontASCIIRunes) {
		return false
	}
	if isMonospace {
		return allAdvancesNonZero(asciiAdvances)
	}
	return equalAdvances(asciiAdvances)
}

func equalAdvances(advances []float32) bool {
	if len(advances) == 0 {
		return false
	}
	first := advances[0]
	if first <= 0 {
		return false
	}
	for _, advance := range advances[1:] {
		if advance != first {
			return false
		}
	}
	return true
}

func allAdvancesNonZero(advances []float32) bool {
	if len(advances) == 0 {
		return false
	}
	for _, advance := range advances {
		if advance <= 0 {
			return false
		}
	}
	return true
}
