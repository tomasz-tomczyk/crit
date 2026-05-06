'use strict';
// Parity-fix coverage: exercises the contract changes that close gaps
// between code-review (app.js) and design-mode (design-mode.js +
// design-mode.row.js).
//
// Scope:
//   1. Resolved design pins auto-collapse (collapseDefault: !!c.resolved).
//   2. agent-marker.css carries the pin-mode pointer-events override.
//   3. crit-agent's setMode toggles .crit-marker-root--pin-mode on the
//      overlay root (Pin: pass-through; Navigate: clickable for deep-link).
//
// We don't pull in a full DOM — the comment-card test reuses a shim defined
// in crit-comment-card.test.js, but loading test files cross-imports their
// global state. So we keep this isolated with a minimal local shim.

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

function makeEl() {
  const el = {
    children: [],
    classList: {
      _set: new Set(),
      add(...c) { c.forEach((x) => this._set.add(x)); },
      remove(...c) { c.forEach((x) => this._set.delete(x)); },
      contains(c) { return this._set.has(c); },
      toggle(c, force) {
        if (force === true) { this._set.add(c); return true; }
        if (force === false) { this._set.delete(c); return false; }
        if (this._set.has(c)) { this._set.delete(c); return false; }
        this._set.add(c); return true;
      },
    },
    dataset: {},
    style: {},
    listeners: {},
    appendChild(c) { this.children.push(c); return c; },
    prepend(c) { this.children.unshift(c); return c; },
    addEventListener(name, fn) { this.listeners[name] = fn; },
    setAttribute(k, v) { this.attrs = this.attrs || {}; this.attrs[k] = v; },
    set className(v) { this._cn = v; v.split(/\s+/).forEach((c) => this.classList._set.add(c)); },
    get className() { return this._cn; },
    set innerHTML(v) { this._html = v; },
    get innerHTML() { return this._html; },
    set textContent(v) { this._text = v; },
    get textContent() { return this._text; },
    set id(v) { this._id = v; },
    get id() { return this._id; },
  };
  return el;
}

global.document = global.document || { createElement: () => makeEl() };

const card = require('../crit-comment-card.js');

function baseDeps() {
  return {
    commentMd: { render: (b) => '<p>' + (b || '') + '</p>' },
    formatTime: () => '12:00',
    authorColorIndex: () => 1,
    getReviewRound: () => 1,
    getAgentName: () => 'agent',
    buildCommentEnv: () => ({}),
    renderReplyList: () => makeEl(),
    createReplyInput: () => makeEl(),
    iconChevron: '<svg/>',
  };
}

test('resolved design pin defaults to collapsed (collapseDefault: true)', () => {
  // Mirrors design-mode.row.js: `collapseDefault: !!c.resolved`. With no
  // override and a resolved comment, the card should boot collapsed.
  const out = card.buildCommentCard(
    { id: 'c1', body: 'x', resolved: true, created_at: '2024-01-01T00:00:00Z' },
    '/route',
    {
      deps: baseDeps(),
      collapseDefault: true,
      getCollapseOverride: () => undefined,
      setCollapseOverride: () => {},
    }
  );
  assert.equal(out.card.classList.contains('collapsed'), true);
});

test('open design pin stays expanded (collapseDefault: false)', () => {
  const out = card.buildCommentCard(
    { id: 'c2', body: 'x', resolved: false, created_at: '2024-01-01T00:00:00Z' },
    '/route',
    {
      deps: baseDeps(),
      collapseDefault: false,
      getCollapseOverride: () => undefined,
      setCollapseOverride: () => {},
    }
  );
  assert.equal(out.card.classList.contains('collapsed'), false);
});

test('agent-marker.css disables marker pointer-events in pin mode', () => {
  // The CSS rule lives in agent-marker.css and is toggled via a class on
  // #crit-marker-root from crit-agent.js. We assert the rule is present so
  // pin-mode hover passes through markers to the underlying element.
  const css = fs.readFileSync(path.join(__dirname, '..', 'agent-marker.css'), 'utf8');
  assert.match(css, /\.crit-marker-root--pin-mode\s+\.crit-design-marker\s*\{[^}]*pointer-events\s*:\s*none/);
});

test('crit-agent setMode toggles pin-mode class on overlay root', () => {
  // Static check — guards the contract that setMode flips the class on
  // state.overlay.root. Without this, the CSS rule above would never fire.
  const js = fs.readFileSync(path.join(__dirname, '..', 'crit-agent.js'), 'utf8');
  assert.match(
    js,
    /state\.overlay\.root\.classList\.toggle\(\s*['"]crit-marker-root--pin-mode['"]\s*,\s*value === 'pin'\s*\)/
  );
});

test('design-mode expand-all button updates textContent label', () => {
  // Mirrors app.js#updateExpandAllLabel — the design-mode handler must flip
  // the visible button text in addition to aria-pressed and title. The
  // handler now lives in design-mode.panel-render.js.
  const js = fs.readFileSync(path.join(__dirname, '..', 'design-mode.panel-render.js'), 'utf8');
  assert.match(
    js,
    /expandBtn\.textContent\s*=\s*state\.designExpandAll\s*\?\s*['"]Collapse all['"]\s*:\s*['"]Expand all['"]/
  );
});

test('design-mode comment-count button gets dynamic title (parity with app.js)', () => {
  // The navbar pill (count text + dynamic title + resolved-state class) is
  // now driven by the shared helper crit.shared.updateCommentCountIndicator,
  // so design-mode and app.js can't drift. Verify both modes call it and
  // the helper itself owns the strings. Design-mode call lives in the
  // panel-render module.
  const dm = fs.readFileSync(path.join(__dirname, '..', 'design-mode.panel-render.js'), 'utf8');
  const app = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
  const shared = fs.readFileSync(path.join(__dirname, '..', 'crit-shared.js'), 'utf8');
  assert.match(dm, /updateCommentCountIndicator/);
  assert.match(app, /updateCommentCountIndicator/);
  assert.match(shared, /unresolved comment/);
  assert.match(shared, /resolved comment/);
  assert.match(shared, /comment-count-resolved/);
});
