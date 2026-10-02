// Helpers for OpenCode wait-notify: detect foreground `crit` wait commands
// and build toast copy. Shared by crit.ts and unit tests.
'use strict';

/**
 * Returns true when a bash command is a blocking crit review wait
 * (not comment/config/share/etc. subcommands).
 * @param {string} command
 * @returns {boolean}
 */
function isCritWaitCommand(command) {
  if (typeof command !== 'string') return false;
  const trimmed = command.trim();
  // Allow optional env prefixes like FOO=bar crit ...
  const withoutEnv = trimmed.replace(/^(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)+/, '');
  if (!/^(?:\.\/)?crit(?:\s|$)/.test(withoutEnv)) return false;

  const rest = withoutEnv.replace(/^(?:\.\/)?crit\s*/, '').trim();
  if (rest === '') return true;

  // First token is a known non-wait subcommand (see commandDispatch in
  // cmd/crit/cli_dispatch.go). Wait entrypoints are bare `crit`, path args,
  // review flags (--pr/--range/...), and live/preview/review/plan.
  const first = rest.split(/\s+/)[0];
  const nonWait = new Set([
    'help',
    '--help',
    '-h',
    '--version',
    '-v',
    'version',
    'share',
    'fetch',
    'unpublish',
    'install',
    'config',
    'check',
    'pr',
    'pull',
    'push',
    'comment',
    'comments',
    'plan-hook',
    'story',
    'auth',
    'stop',
    'status',
    'stats',
    'cleanup',
    '_serve',
  ]);
  if (nonWait.has(first)) return false;

  // Flags that still start a review wait are fine (e.g. --pr, --range).
  // Subcommands live/preview/review/plan are waits.
  return true;
}

/**
 * @param {string} [url]
 * @returns {{ title: string, message: string }}
 */
function roundReadyToast(url) {
  const message = url
    ? `Review ready — open ${url}`
    : 'Review ready — check your browser and click Finish Review when done.';
  return { title: 'Crit', message };
}

module.exports = { isCritWaitCommand, roundReadyToast };
