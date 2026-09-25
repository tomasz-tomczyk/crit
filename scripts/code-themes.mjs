// Crit's code themes: Shiki themes adjusted for WCAG AA contrast on every
// background Pierre paints code on. Built into the vendored bundle by
// scripts/build-pierre.mjs (registered as crit-dark / crit-light).
//
// Bases: Tokyo Night (Crit's editor palette) and GitHub Light Default. Pierre
// tints lines for additions/deletions and word-level changes, and a token
// colour that reads fine on the plain background can fall under 4.5:1 on
// those tints. Each token colour below the bar is moved toward white (dark)
// or black (light) until it clears the darkest/lightest background, keeping
// its hue family.

import tokyoNight from "@shikijs/themes/tokyo-night";
import githubLightDefault from "@shikijs/themes/github-light-default";

// Backgrounds under code in @pierre/diffs 1.5.1 with these bases, measured in
// Chromium: plain, added line, deleted line, word-level added, word-level
// deleted. Re-measure when bumping Pierre or a base theme.
export const CODE_BACKGROUNDS = {
  dark: ["#1a1b26", "#24323e", "#31252f", "#2a4753", "#442c36"],
  light: ["#ffffff", "#e5efe5", "#fee7e3", "#c7decb", "#f7c9c8"],
};

export const MIN_CONTRAST = 4.6; // WCAG AA (4.5) plus rounding headroom

function channel(v) {
  const c = v / 255;
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}

function rgb(hex) {
  return [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16));
}

export function luminance(hex) {
  const [r, g, b] = rgb(hex).map(channel);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function contrast(a, b) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

function mix(a, b, t) {
  const [ca, cb] = [rgb(a), rgb(b)];
  return "#" + ca.map((v, i) => Math.round(v + (cb[i] - v) * t).toString(16).padStart(2, "0")).join("");
}

// The colour, moved toward `toward` in 2% steps until it reaches
// MIN_CONTRAST on every background.
export function readable(hex, backgrounds, toward) {
  const worst = c => Math.min(...backgrounds.map(bg => contrast(c, bg)));
  let out = hex;
  for (let t = 0.02; worst(out) < MIN_CONTRAST && t <= 1; t += 0.02) out = mix(hex, toward, t);
  return out;
}

const HEX6 = /^#[0-9a-f]{6}$/i;

export function withContrast(theme, name, kind) {
  const backgrounds = CODE_BACKGROUNDS[kind];
  const toward = kind === "dark" ? "#ffffff" : "#000000";
  const fix = c => (typeof c === "string" && HEX6.test(c) ? readable(c.toLowerCase(), backgrounds, toward) : c);
  return {
    ...theme,
    name,
    colors: { ...theme.colors, "editor.foreground": fix(theme.colors["editor.foreground"]) },
    tokenColors: theme.tokenColors.map(tc =>
      tc.settings && tc.settings.foreground
        ? { ...tc, settings: { ...tc.settings, foreground: fix(tc.settings.foreground) } }
        : tc),
  };
}

export function critCodeThemes() {
  return {
    dark: withContrast(tokyoNight, "crit-dark", "dark"),
    light: withContrast(githubLightDefault, "crit-light", "light"),
  };
}
