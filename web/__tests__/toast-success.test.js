'use strict';
// Regression for #1087: showToast sets class "toast toast-<type>", but the
// success rules were removed as dead CSS after the class changed from
// "toast success" to "toast-success". Error toasts kept a matching rule;
// success toasts (Comments pulled, Re-shared, Local share link cleared)
// rendered as bare text over the header.

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

function ruleBlock(css, selector) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const match = css.match(new RegExp(escaped + '\\s*\\{[^}]*\\}'));
  assert.ok(match, 'expected a ' + selector + ' rule');
  return match[0];
}

test('style.css: .toast.toast-success is an elevated surface, not bare text', () => {
  const css = fs.readFileSync(path.join(__dirname, '../style.css'), 'utf8');
  const rule = ruleBlock(css, '.toast.toast-success');
  assert.match(rule, /background:\s*var\(--crit-bg-elevated\)/);
  assert.match(rule, /border:\s*1px solid var\(--crit-border\)/);
  assert.match(rule, /color:\s*var\(--crit-fg-secondary\)/);
});

test('style.css: success toast filled buttons use the green token', () => {
  const css = fs.readFileSync(path.join(__dirname, '../style.css'), 'utf8');
  const rule = ruleBlock(css, '.toast.toast-success .toast-btn-filled');
  assert.match(rule, /background:\s*var\(--crit-green\)/);
});
