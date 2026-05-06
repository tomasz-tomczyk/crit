const test = require('node:test');
const assert = require('node:assert/strict');
const utils = require('../design-route-utils.js');

// Re-implement recordRoute purely to test idempotency of the algorithm
// design-mode.js uses (mirrors lines in design-mode.js's recordRoute).
function makeRecorder() {
  const state = { routes: [], unsavedRoutes: new Set(), comments: [], currentRoute: '/' };
  function recordRoute(pathname) {
    const route = utils.normaliseRoute(pathname || '/');
    state.currentRoute = route;
    if (state.routes.indexOf(route) === -1) {
      state.routes.push(route);
      const known = new Set(state.comments.map(c => utils.normaliseRoute(c.path || '/')));
      if (!known.has(route)) state.unsavedRoutes.add(route);
    } else {
      const known2 = new Set(state.comments.map(c => utils.normaliseRoute(c.path || '/')));
      if (known2.has(route)) state.unsavedRoutes.delete(route);
    }
    return state;
  }
  return { state, recordRoute };
}

test('recordRoute is idempotent on repeated visits', () => {
  const r = makeRecorder();
  r.recordRoute('/dash');
  r.recordRoute('/dash');
  r.recordRoute('/dash/');
  assert.deepEqual(r.state.routes, ['/dash']);
});

test('recordRoute classifies route as unsaved if no comment matches', () => {
  const r = makeRecorder();
  r.state.comments = [{ path: '/billing' }];
  r.recordRoute('/dash');
  assert.equal(r.state.unsavedRoutes.has('/dash'), true);
  r.recordRoute('/billing');
  assert.equal(r.state.unsavedRoutes.has('/billing'), false);
});
