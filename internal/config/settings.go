package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	maxCodeFontLength     = 256
	systemCodeFontStack   = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
	maxShortcutBindingLen = 64
)

var (
	codeFontForbiddenChars = regexp.MustCompile(`[;{}<>@]`)
	codeFontForbiddenFuncs = regexp.MustCompile(`(?i)(?:url|expression)\s*\(`)
)

// allowedSettingEnums maps enum-typed UI settings to their valid string values.
var allowedSettingEnums = map[string][]string{
	"theme":            {"system", "light", "dark"},
	"codeOverflow":     {"scroll", "wrap"},
	"width":            {"compact", "default", "wide"},
	"diffMode":         {"split", "unified"},
	"inlineDiff":       {"word-alt", "word", "char", "none"},
	"unchangedContext": {"collapsed", "expanded"},
	"diffScope":        {"all", "branch", "staged", "unstaged"},
	"live_viewport":    {"mobile", "tablet", "desktop", "fit"},
	"fileTree":         {"open", "collapsed"},
	"toc":              {"open", "closed"},
	"commentsPanel":    {"open", "collapsed"},
}

// allowedSettingOnOff lists settings stored as "on"/"off" in the cookie that
// also accept booleans in config (true -> "on", false -> "off").
var allowedSettingOnOff = map[string]bool{
	"boostContrast": true,
	"lineNumbers":   true,
}

// allowedSettingBools lists boolean-typed UI settings.
var allowedSettingBools = map[string]bool{
	"ignoreWhitespace":  true,
	"hideResolved":      true,
	"live_hideResolved": true,
}

// allowedShortcutIDs lists every customizable shortcut action ID defined in
// web/crit-shortcuts.js (fixed shortcuts have no ID and cannot be rebound).
var allowedShortcutIDs = map[string]bool{
	"comment":           true,
	"delete_comment":    true,
	"edit_comment":      true,
	"finish_review":     true,
	"general_comment":   true,
	"next_block":        true,
	"next_change":       true,
	"next_comment":      true,
	"next_commit":       true,
	"previous_block":    true,
	"previous_change":   true,
	"previous_comment":  true,
	"previous_commit":   true,
	"scope_all":         true,
	"scope_branch":      true,
	"scope_staged":      true,
	"scope_unstaged":    true,
	"story_next":        true,
	"story_previous":    true,
	"story_prologue":    true,
	"story_support":     true,
	"story_toggle_list": true,
	"toggle_collapse":   true,
	"toggle_comments":   true,
	"toggle_file_tree":  true,
	"toggle_pin_mode":   true,
	"toggle_resolved":   true,
	"toggle_toc":        true,
	"toggle_viewed":     true,
	"visual_mode":       true,
}

var reservedShortcutKeys = map[string]bool{
	"Esc":   true,
	"Tab":   true,
	"Enter": true,
	"1":     true,
	"2":     true,
	"3":     true,
	"4":     true,
	"5":     true,
	"6":     true,
	"7":     true,
	"8":     true,
	"9":     true,
}

// parseAndValidateSettings parses and validates the global "settings" object
// from a config file. Invalid keys or values print a warning to stderr and are
// skipped so valid entries still take effect.
func parseAndValidateSettings(raw json.RawMessage, path string) map[string]any {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: invalid settings in %s (want a JSON object); ignoring\n", path)
		return nil
	}
	if len(obj) == 0 {
		return nil
	}

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	valid := make(map[string]any, len(obj))
	for _, k := range keys {
		val, ok, reason := validateSettingValue(k, obj[k], path)
		if !ok {
			if reason != "" {
				fmt.Fprintf(os.Stderr, "Warning: %s in %s; ignoring\n", reason, path)
			}
			continue
		}
		valid[k] = val
	}
	if len(valid) == 0 {
		return nil
	}
	return valid
}

func validateSettingValue(key string, raw any, path string) (any, bool, string) {
	if allowed, ok := allowedSettingEnums[key]; ok {
		return validateEnumSetting(key, raw, allowed)
	}
	if allowedSettingOnOff[key] {
		return validateOnOffSetting(key, raw)
	}
	if allowedSettingBools[key] {
		return validateBoolSetting(key, raw)
	}

	switch key {
	case "lightPalette", "darkPalette":
		return validatePaletteSetting(key, raw)
	case "codeFont":
		return validateCodeFontSetting(raw)
	case "shortcuts":
		return validateShortcutsValue(raw, path)
	default:
		return nil, false, "unknown setting settings." + key
	}
}

func validateEnumSetting(key string, raw any, allowed []string) (any, bool, string) {
	s, isStr := raw.(string)
	if !isStr || !slices.Contains(allowed, s) {
		return nil, false, fmt.Sprintf("invalid settings.%s %s (want one of %s)", key, formatSettingValue(raw), formatQuotedList(allowed))
	}
	return s, true, ""
}

func validateOnOffSetting(key string, raw any) (any, bool, string) {
	switch v := raw.(type) {
	case string:
		if v == "on" || v == "off" {
			return v, true, ""
		}
	case bool:
		if v {
			return "on", true, ""
		}
		return "off", true, ""
	}
	return nil, false, fmt.Sprintf("invalid settings.%s %s (want \"on\", \"off\", or boolean)", key, formatSettingValue(raw))
}

func validateBoolSetting(key string, raw any) (any, bool, string) {
	b, ok := raw.(bool)
	if !ok {
		return nil, false, fmt.Sprintf("invalid settings.%s %s (want boolean)", key, formatSettingValue(raw))
	}
	return b, true, ""
}

func validatePaletteSetting(key string, raw any) (any, bool, string) {
	s, ok := raw.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil, false, fmt.Sprintf("invalid settings.%s %s (want non-empty palette ID string)", key, formatSettingValue(raw))
	}
	return strings.TrimSpace(s), true, ""
}

func validateCodeFontSetting(raw any) (any, bool, string) {
	s, ok := raw.(string)
	if !ok {
		return nil, false, fmt.Sprintf("invalid settings.codeFont %s (want font family string)", formatSettingValue(raw))
	}
	normalized, valid := normalizeCodeFontSetting(s)
	if !valid {
		return nil, false, fmt.Sprintf("invalid settings.codeFont %q (must be a valid CSS font-family value <= %d chars)", s, maxCodeFontLength)
	}
	return normalized, true, ""
}

func validateShortcutsValue(raw any, path string) (any, bool, string) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, false, fmt.Sprintf("invalid settings.shortcuts %s (want JSON object mapping action IDs to bindings)", formatSettingValue(raw))
	}
	cleaned := validateShortcutsSetting(obj, path)
	if len(cleaned) == 0 {
		return nil, false, ""
	}
	return cleaned, true, ""
}

func normalizeCodeFontSetting(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	if v == "" || strings.EqualFold(v, "default") {
		return "", true
	}
	if strings.EqualFold(v, "system") {
		return systemCodeFontStack, true
	}
	if len(v) > maxCodeFontLength || hasControlChar(v) {
		return "", false
	}
	if codeFontForbiddenChars.MatchString(v) || codeFontForbiddenFuncs.MatchString(v) {
		return "", false
	}
	return v, true
}

func validateShortcutsSetting(raw map[string]any, path string) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	cleaned := make(map[string]string, len(raw))
	for _, id := range keys {
		if !allowedShortcutIDs[id] {
			fmt.Fprintf(os.Stderr, "Warning: unknown shortcut settings.shortcuts.%s in %s; ignoring\n", id, path)
			continue
		}
		binding, ok := raw[id].(string)
		if !ok {
			fmt.Fprintf(os.Stderr, "Warning: invalid shortcut settings.shortcuts.%s %s in %s (want string); ignoring\n", id, formatSettingValue(raw[id]), path)
			continue
		}
		if binding != "" && !validShortcutBinding(binding) {
			fmt.Fprintf(os.Stderr, "Warning: invalid or reserved shortcut settings.shortcuts.%s %q in %s; ignoring\n", id, binding, path)
			continue
		}
		cleaned[id] = binding
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

// validShortcutBinding mirrors isReservedBinding in web/crit-shortcuts.js and
// rejects control characters or overly long strings.
func validShortcutBinding(binding string) bool {
	if len(binding) > maxShortcutBindingLen || hasControlChar(binding) {
		return false
	}
	if reservedShortcutKeys[binding] {
		return false
	}
	return !isReservedShortcutChord(binding)
}

func isReservedShortcutChord(binding string) bool {
	parts := strings.Split(binding, "+")
	last := parts[len(parts)-1]
	mods := parts[:len(parts)-1]
	if last == "/" && slices.Contains(mods, "Shift") {
		return true
	}
	if last == "Enter" && (slices.Contains(mods, "Ctrl") || slices.Contains(mods, "Meta")) {
		return true
	}
	return false
}

func hasControlChar(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// FilterSettingsPalettes validates lightPalette and darkPalette entries in
// settings against the shipped light and dark palette IDs. Invalid entries
// print a warning to stderr and are removed in place.
func FilterSettingsPalettes(settings map[string]any, lightIDs, darkIDs map[string]bool, source string) {
	if len(settings) == 0 {
		return
	}
	filterPaletteKey(settings, "lightPalette", lightIDs, source)
	filterPaletteKey(settings, "darkPalette", darkIDs, source)
}

func filterPaletteKey(settings map[string]any, key string, validIDs map[string]bool, source string) {
	v, ok := settings[key]
	if !ok {
		return
	}
	s, isStr := v.(string)
	if !isStr || !validIDs[s] {
		fmt.Fprintf(os.Stderr, "Warning: unknown settings.%s %q in %s; ignoring\n", key, s, source)
		delete(settings, key)
	}
}

func formatQuotedList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = strconv.Quote(item)
	}
	return strings.Join(quoted, ", ")
}

func formatSettingValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
