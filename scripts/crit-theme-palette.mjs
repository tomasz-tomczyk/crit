// Shiki/VS Code colour input -> Crit foundations. Syntax tokens are untouched.
// Keep this mapping separate from component CSS and from the renderer adapter.
// Colour maths lives in web/crit-theme-boost.js, shared with the runtime.
import { createRequire } from 'node:module';
const { parse, mix, luminance, contrast, oklch, fromOklch, distance } = createRequire(import.meta.url)('../web/crit-theme-boost.js');
export { distance };
// Shiki themes use CSS hex in short, long and alpha forms. Translucent role
// colours are flattened onto the editor background rather than discarded.
const color = parse;
export function themePalette(theme) {
  const colors = theme.colors || {};
  const fallbackBg = theme.type === 'light' ? '#ffffff' : '#1a1b26';
  const bg = color(theme.bg, fallbackBg) || color(colors['editor.background'], fallbackBg) || fallbackBg;
  const pick = (keys, fallback) => keys.map(key => color(colors[key], bg)).find(Boolean) || fallback;
  const editorFg = color(theme.fg, bg) || pick(['editor.foreground'], theme.type === 'light' ? '#24292f' : '#c0caf5');
  // VS Code sidebars/inputs may invert the editor's palette. Their backgrounds
  // cannot be reused without their companion foregrounds. Crit's ordinary
  // cards share the editor foreground, so derive a cohesive surface hierarchy.
  const surface = mix(editorFg, bg, 0.035);
  const elevated = mix(editorFg, bg, 0.07);
  const backgrounds = [bg, surface, elevated, mix(editorFg, elevated, 0.05)];
  // Some source editor foregrounds themselves have low contrast. Adjust only
  // Crit's UI foreground, leaving Pierre/Shiki's original theme untouched.
  const endpoint = contrast('#000000', bg) > contrast('#ffffff', bg) ? '#000000' : '#ffffff';
  const fg = readable(editorFg, backgrounds, endpoint);
  const readableRole = value => readable(value, backgrounds, fg);
  const hueRole = value => readableHue(value, backgrounds, fg);
  const accent = readable(pick(['textLink.foreground', 'button.background', 'focusBorder'], fg), backgrounds, fg, true);
  const hues = {
    red: hueRole(pick(['terminal.ansiRed', 'editorError.foreground'], '#d73a49')),
    green: hueRole(pick(['terminal.ansiGreen', 'editorGutter.addedBackground'], '#22863a')),
    yellow: hueRole(pick(['terminal.ansiYellow', 'editorWarning.foreground'], '#b08800')),
    orange: hueRole(pick(['editorGutter.modifiedBackground'], '#d18616')),
    purple: hueRole(pick(['terminal.ansiMagenta'], '#8957e5')),
    blue: hueRole(pick(['terminal.ansiBlue'], '#0969da')),
    cyan: hueRole(pick(['terminal.ansiCyan'], '#1b7c83')),
  };
  return {
    id: theme.name, type: theme.type, displayName: theme.displayName || theme.name,
    colors: {
      bg, fg, accent,
      'on-accent': contrast(accent, '#000000') > contrast(accent, '#ffffff') ? '#000000' : '#ffffff',
      muted: readableRole(pick(['descriptionForeground', 'editorLineNumber.foreground'], mix(fg, bg, 0.65))),
      // Not panel.border: themes design that for one divider line, so it can
      // be an accent (Dracula) or match the background (Material, Rose Pine).
      // Crit borders every card and file, so derive a faint neutral one.
      border: borderFor(endpoint, bg, /high-contrast/.test(theme.name || '') ? 2.2 : BORDER_CONTRAST),
      surface, elevated,
      ...hues,
      ...authorColors(hues, backgrounds, fg),
      shadow: theme.type === 'light' ? 'rgba(0, 0, 0, 0.12)' : 'rgba(0, 0, 0, 0.35)',
      overlay: theme.type === 'light' ? 'rgba(0, 0, 0, 0.3)' : 'rgba(0, 0, 0, 0.6)',
    },
  };
}

// Author tags (comment-card-helpers hashes a name to 0-5): the theme's own
// terminal colours in the classic order. Some themes reuse a colour (Synthwave
// blue = cyan) or have near-identical ones; such a tag gets the classic hue at
// the theme's usual lightness and colourfulness instead.
const AUTHOR_HUES = [['blue', 250], ['red', 25], ['green', 145], ['yellow', 90], ['purple', 305], ['cyan', 195]];
export const AUTHOR_MIN_DISTANCE = 0.06;
function authorColors(hues, backgrounds, fg) {
  const lch = AUTHOR_HUES.map(([role]) => oklch(hues[role]));
  const middle = values => values.slice().sort((a, b) => a - b)[values.length >> 1];
  const lightness = middle(lch.map(c => c[0]));
  const chroma = Math.min(0.18, Math.max(0.12, middle(lch.map(c => c[1]))));
  const chosen = [];
  const nearest = candidate => Math.min(...chosen.map(other => distance(candidate, other)), Infinity);
  const out = {};
  AUTHOR_HUES.forEach(([role, hue], i) => {
    let value = hues[role];
    if (nearest(value) < AUTHOR_MIN_DISTANCE) {
      // Classic hue first; otherwise the nearby hue that stands apart most.
      let best = null;
      for (const shift of [0, 15, -15, 30, -30, 45, -45, 60, -60]) {
        const candidate = readableHue(fromOklch(lightness, chroma, hue + shift), backgrounds, fg);
        if (!best || nearest(candidate) > nearest(best)) best = candidate;
        if (nearest(candidate) >= AUTHOR_MIN_DISTANCE) break;
      }
      value = best;
    }
    chosen.push(value);
    out['author-' + i] = value;
  });
  return out;
}

// Status and tag colours: keep the theme's hue and colourfulness and move
// only lightness until the colour reads on every card surface and on its own
// 15% tint (tags, badges). Blending toward the foreground instead turns dim
// colours grey (Solarized Dark's red came out grey).
function readableHue(value, backgrounds, fg) {
  const [l, c, h] = oklch(value);
  const lighten = luminance(backgrounds[0]) < 0.2;
  for (let step = 0; step <= 100; step++) {
    const target = lighten ? l + step / 100 : l - step / 100;
    if (target < 0 || target > 1) break;
    const candidate = fromOklch(target, c, h);
    if (backgrounds.every(bg => contrast(candidate, bg) >= 4.5 && contrast(candidate, mix(candidate, bg, 0.15)) >= 4.5)) return candidate;
  }
  return readable(value, backgrounds, fg);
}

// Card and file borders: faint but always visible on the editor background.
// Blend toward black/white, not the editor foreground, which can be coloured
// (min-dark's is purple); the background keeps its own tint.
export const BORDER_CONTRAST = 1.45;
function borderFor(endpoint, bg, target) {
  for (let share = 1; share <= 100; share++) {
    const candidate = mix(endpoint, bg, share / 100);
    if (contrast(candidate, bg) >= target) return candidate;
  }
  return endpoint;
}

function readable(color, backgrounds, fg, tinted = false) {
  // Only Crit UI role colours, never Shiki token colours. Sparse themes remain usable.
  for (let share = 0; share <= 10; share++) {
    const candidate = mix(fg, color, share / 10);
    // Check actual card/hover surfaces, not an arbitrary higher editor target.
    if (backgrounds.every(bg => contrast(candidate, bg) >= 4.5 && (!tinted || contrast(candidate, mix(candidate, bg, 0.2)) >= 4.5))) return candidate;
  }
  return fg;
}
