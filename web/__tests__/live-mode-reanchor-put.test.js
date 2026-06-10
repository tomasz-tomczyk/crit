'use strict';
const test = require('node:test');
const assert = require('node:assert');
const { buildReanchorRequest } = require('../live-mode-reanchor-put.js');

test('builds PUT to /api/comment/{id}?path=<pathname> with dom_anchor body', () => {
  const r = buildReanchorRequest('p1', { pathname: '/dashboard', css_selector: 'h1', tag_chain: ['H1'] });
  assert.equal(r.method, 'PUT');
  assert.equal(r.url, '/api/comment/p1?path=%2Fdashboard');
  const body = JSON.parse(r.body);
  assert.deepEqual(body.dom_anchor.tag_chain, ['H1']);
});

test('encodes special characters in pin id and path', () => {
  const r = buildReanchorRequest('p/1?', { pathname: '/foo/bar?q=1' });
  assert.match(r.url, /^\/api\/comment\/p%2F1%3F\?path=/);
});
