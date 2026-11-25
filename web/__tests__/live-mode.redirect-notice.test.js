'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { openableURL } = require('../live-mode.redirect-notice.js');

const SELF = 'http://127.0.0.1:4567';

test('opens http(s) URLs on another origin', () => {
  assert.equal(openableURL('https://example.com/login?next=/', SELF), 'https://example.com/login?next=/');
  assert.equal(openableURL('http://localhost:3000/', SELF), 'http://localhost:3000/');
  // Another port on the same host is another origin (live mode's app).
  assert.equal(openableURL('http://127.0.0.1:4568/x', SELF), 'http://127.0.0.1:4568/x');
});

test('refuses the daemon origin, where the page would run unsandboxed', () => {
  assert.equal(openableURL('/files/mockup.html', SELF), '');
  assert.equal(openableURL(SELF + '/preview-content/', SELF), '');
  assert.equal(openableURL('//127.0.0.1:4567/files/x.html', SELF), '');
});

test('refuses non-http schemes', () => {
  assert.equal(openableURL('javascript:alert(1)', SELF), '');
  assert.equal(openableURL('data:text/html,<script>1</script>', SELF), '');
  assert.equal(openableURL('file:///etc/passwd', SELF), '');
  assert.equal(openableURL('blob:http://127.0.0.1:4567/abc', SELF), '');
});

test('refuses empty and malformed input', () => {
  assert.equal(openableURL('', SELF), '');
  assert.equal(openableURL(null, SELF), '');
  assert.equal(openableURL('http://[bad', SELF), '');
});
