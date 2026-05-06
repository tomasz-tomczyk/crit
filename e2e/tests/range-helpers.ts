import { expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'node:fs';
import { stateFilePath } from './state-file';

// Fixture state written by setup-fixtures-range-mode.sh. Cached after the
// first read since the fixture's SHAs don't change for the lifetime of the
// daemon.
type RangeFixtureState = {
  base: string;       // A
  head: string;       // B (the canonical layer head — what `--range A..B` boots with)
  defaultSHA: string; // main
  headAfter: string;  // D (a head that diverges from B; rewrites b.txt for stale tests)
  fixtureDir: string;
};

let cachedState: RangeFixtureState | undefined;

function readState(): RangeFixtureState {
  if (cachedState) return cachedState;
  const port = process.env.CRIT_TEST_RANGE_PORT || '3128';
  const text = fs.readFileSync(stateFilePath(port), 'utf8');
  const grab = (key: string) => {
    const m = text.match(new RegExp(`^${key}=(.+)$`, 'm'));
    if (!m) throw new Error(`fixture state missing ${key}`);
    return m[1];
  };
  cachedState = {
    base: grab('RANGE_BASE'),
    head: grab('RANGE_HEAD'),
    defaultSHA: grab('RANGE_DEFAULT'),
    headAfter: grab('RANGE_HEAD_AFTER'),
    fixtureDir: grab('CRIT_FIXTURE_DIR'),
  };
  return cachedState;
}

// rangeFixture returns the canonical SHAs the rangemode fixture was booted
// with. Throws if the fixture state file isn't present (which would mean
// setup-fixtures-range-mode.sh didn't run).
export function rangeFixture(): RangeFixtureState {
  return readState();
}

// ensureRangeFocus resets the daemon to the canonical layer-scope focus the
// fixture booted with. Use in beforeEach for any test that needs a clean
// range starting state — earlier tests may have switched to working tree or
// flipped to full-stack scope.
export async function ensureRangeFocus(request: APIRequestContext) {
  const s = readState();
  const post = await request.post('/api/focus', {
    data: {
      kind: 'range',
      base_sha: s.base,
      head_sha: s.head,
      diff_scope: 'layer',
    },
  });
  expect(post.ok()).toBeTruthy();
}

// ensureStackedFocus puts the daemon in the layer scope of a synthesized
// stacked PR (is_stacked=true, default_sha set). Use this when a test needs
// the layer/full-stack toggle UI to be visible.
export async function ensureStackedFocus(request: APIRequestContext, scope: 'layer' | 'full_stack' = 'layer') {
  const s = readState();
  const post = await request.post('/api/focus', {
    data: {
      kind: 'range',
      base_sha: s.base,
      head_sha: s.head,
      default_sha: s.defaultSHA,
      diff_scope: scope,
      is_stacked: true,
    },
  });
  expect(post.ok()).toBeTruthy();
}
