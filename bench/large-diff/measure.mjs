// THROWAWAY SPIKE: measure Crit's production large-diff path in headless Chromium.
import { spawn } from 'node:child_process';
import { createRequire } from 'node:module';
import { cpus, platform, arch, tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { performance } from 'node:perf_hooks';
import { setTimeout as delay } from 'node:timers/promises';

import {
  CHANGE_EVERY,
  EXPECTED_CHANGED_LINES,
  SOURCE_LINES,
  makeLargeFixture,
} from './fixture.mjs';

const require = createRequire(import.meta.url);
let chromium;
try {
  ({ chromium } = require('../../test/e2e/node_modules/playwright'));
} catch (error) {
  throw new Error(
    'Playwright is not installed. Run `cd test/e2e && npm ci && npx playwright install chromium` first.',
    { cause: error },
  );
}

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(scriptDir, '../..');
const host = '127.0.0.1';
const port = Number.parseInt(process.env.CRIT_MEASURE_PORT || '4179', 10);
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error(`Invalid CRIT_MEASURE_PORT: ${process.env.CRIT_MEASURE_PORT}`);
}
const origin = `http://${host}:${port}`;
const validateOnly = process.argv.includes('--validate-only');

function run(command, args, options = {}) {
  return new Promise((resolveRun, rejectRun) => {
    const child = spawn(command, args, {
      cwd: options.cwd || repoRoot,
      env: options.env || process.env,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    child.on('error', rejectRun);
    child.on('exit', (code, signal) => {
      if (code === 0) {
        resolveRun({ stdout: stdout.trim(), stderr: stderr.trim() });
        return;
      }
      rejectRun(new Error(
        `${command} ${args.join(' ')} failed (${signal || code})\n${stdout}${stderr}`,
      ));
    });
  });
}

async function poll(url, accept, timeoutMs = 120_000) {
  const deadline = performance.now() + timeoutMs;
  let lastError;
  while (performance.now() < deadline) {
    try {
      const response = await fetch(url);
      if (accept(response)) return response;
    } catch (error) {
      lastError = error;
    }
    await delay(50);
  }
  throw new Error(`Timed out waiting for ${url}`, { cause: lastError });
}

function percentile(values, fraction) {
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * fraction))];
}

async function prepareFixture(root) {
  const fixtureDir = join(root, 'fixture');
  await mkdir(fixtureDir);
  await run('git', ['init', '-q', '-b', 'main'], { cwd: fixtureDir });
  await run('git', ['config', 'user.email', 'measure@example.invalid'], { cwd: fixtureDir });
  await run('git', ['config', 'user.name', 'Crit measure harness'], { cwd: fixtureDir });
  await run('git', ['config', 'core.autocrlf', 'false'], { cwd: fixtureDir });

  const fixture = makeLargeFixture();
  const fixturePath = join(fixtureDir, 'synthetic-large.ts');
  await writeFile(fixturePath, fixture.oldContents);
  await run('git', ['add', 'synthetic-large.ts'], { cwd: fixtureDir });
  await run('git', ['commit', '-q', '-m', 'base fixture'], { cwd: fixtureDir });
  await run('git', ['checkout', '-q', '-b', 'measure/large-diff'], { cwd: fixtureDir });
  await writeFile(fixturePath, fixture.newContents);
  await run('git', ['add', 'synthetic-large.ts'], { cwd: fixtureDir });
  await run('git', ['commit', '-q', '-m', 'change every twentieth line'], { cwd: fixtureDir });
  return fixtureDir;
}

async function addMeasurementInit(context) {
  await context.addInitScript(() => {
    window.__CRIT_MEASURE_LONGTASKS__ = [];
    try {
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          window.__CRIT_MEASURE_LONGTASKS__.push({
            startTime: entry.startTime,
            duration: entry.duration,
          });
        }
      }).observe({ type: 'longtask', buffered: true });
    } catch {
      // Long-task entries are supplementary. Core metrics remain available.
    }
  });
}

async function newLayoutPage(browser, layout) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  await context.addCookies([{
    name: 'crit-settings',
    value: encodeURIComponent(JSON.stringify({ diffMode: layout })),
    domain: host,
    path: '/',
    sameSite: 'Strict',
  }]);
  await addMeasurementInit(context);
  return { context, page: await context.newPage() };
}

async function waitForLargeDiffGate(page) {
  const gate = page.locator('.diff-large-placeholder button', { hasText: 'Load diff' });
  await gate.waitFor({ state: 'visible', timeout: 120_000 });
  return page.evaluate(() => performance.now());
}

async function measureMount(page, layout) {
  const result = await page.evaluate(async (expectedLayout) => {
    const active = document.querySelector('#diffModeToggle .toggle-btn.active');
    if (!active || active.dataset.mode !== expectedLayout) {
      throw new Error(`Expected ${expectedLayout} mode, found ${active?.dataset.mode || 'none'}`);
    }
    const button = document.querySelector('.diff-large-placeholder button');
    if (!button) throw new Error('Large-diff gate not found');

    window.__CRIT_MEASURE_LONGTASKS__ = [];
    const start = performance.now();
    button.click();
    const taskEnd = performance.now();
    await new Promise((resolveFrame) => requestAnimationFrame(resolveFrame));
    await new Promise((resolveFrame) => requestAnimationFrame(resolveFrame));
    const paintEnd = performance.now();
    await new Promise((resolveTask) => setTimeout(resolveTask, 0));

    const body = document.querySelector('.file-section .file-body');
    if (!body) throw new Error('Mounted file body not found');
    const tasks = (window.__CRIT_MEASURE_LONGTASKS__ || [])
      .filter((entry) => entry.startTime >= start && entry.startTime <= paintEnd);
    return {
      mount_task_ms: taskEnd - start,
      file_body_first_paint_ms: paintEnd - start,
      mounted_diff_dom_nodes: body.getElementsByTagName('*').length,
      document_dom_nodes: document.getElementsByTagName('*').length,
      rendered_rows: expectedLayout === 'split'
        ? body.querySelectorAll('.diff-split-row').length
        : body.querySelectorAll('.diff-line').length,
      mount_longtask_tbt_ms: tasks.reduce(
        (sum, entry) => sum + Math.max(0, entry.duration - 50), 0,
      ),
      mount_longtask_max_ms: tasks.length
        ? Math.max(...tasks.map((entry) => entry.duration))
        : 0,
    };
  }, layout);
  return result;
}

async function measureScroll(page) {
  const raw = await page.evaluate(async () => {
    const root = document.scrollingElement;
    if (!root) throw new Error('Document scrolling element not found');
    root.style.scrollBehavior = 'auto';
    root.scrollTop = 0;
    await new Promise((resolveFrame) => requestAnimationFrame(resolveFrame));

    const distance = root.scrollHeight - root.clientHeight;
    const samples = [];
    let previous = performance.now();
    const frameCount = 180;
    for (let frame = 0; frame <= frameCount; frame += 1) {
      await new Promise((resolveFrame) => requestAnimationFrame(resolveFrame));
      const now = performance.now();
      if (frame > 0) samples.push(now - previous);
      previous = now;
      root.scrollTop = distance * (frame / frameCount);
    }
    await new Promise((resolveFrame) => requestAnimationFrame(resolveFrame));
    return {
      distance,
      finalScrollTop: root.scrollTop,
      frameIntervalsMs: samples,
    };
  });

  const intervals = raw.frameIntervalsMs;
  return {
    distance_px: raw.distance,
    scroll_reached_bottom: Math.abs(raw.finalScrollTop - raw.distance) < 2,
    frames: intervals.length,
    scroll_mean_frame_ms: intervals.reduce((sum, value) => sum + value, 0) / intervals.length,
    scroll_p95_frame_ms: percentile(intervals, 0.95),
    scroll_worst_frame_ms: Math.max(...intervals),
    scroll_frames_over_32_ms: intervals.filter((value) => value > 32).length,
  };
}

async function stopServer(server) {
  if (!server || server.exitCode != null) return;
  server.kill('SIGTERM');
  await Promise.race([
    new Promise((resolveExit) => server.once('exit', resolveExit)),
    delay(2_000).then(() => {
      if (server.exitCode == null) server.kill('SIGKILL');
    }),
  ]);
}

let tempRoot;
let browser;
let server;
try {
  tempRoot = await mkdtemp(join(tmpdir(), 'crit-large-diff-measure-'));
  const fixtureDir = await prepareFixture(tempRoot);
  const fakeHome = join(tempRoot, 'home');
  const binary = join(tempRoot, 'crit');
  await mkdir(fakeHome);
  await writeFile(join(fakeHome, '.crit.config.json'), JSON.stringify({
    agent_cmd: 'echo',
    disable_stats: true,
    no_update_check: true,
    no_integration_check: true,
  }));
  await run('go', ['build', '-o', binary, './cmd/crit'], {
    cwd: repoRoot,
    env: { ...process.env, GOCACHE: join(tempRoot, 'go-build-cache') },
  });
  const revision = (await run('git', ['rev-parse', '--short', 'HEAD'], { cwd: repoRoot })).stdout;

  if (validateOnly) {
    const rawDiff = (await run(
      'git',
      ['diff', '--unified=3', 'main...HEAD', '--', 'synthetic-large.ts'],
      { cwd: fixtureDir },
    )).stdout;
    const lines = rawDiff.split('\n');
    const actualHunks = lines.filter((line) => line.startsWith('@@ ')).length;
    let inHunk = false;
    let diffLines = 0;
    for (const line of lines) {
      if (line.startsWith('@@ ')) {
        inHunk = true;
      } else if (inHunk && /^[ +\-]/.test(line)) {
        diffLines += 1;
      }
    }
    console.log(JSON.stringify({
      validation_only: true,
      warning: 'Generated git fixture validation only; no server, browser render, or scroll timings were measured.',
      crit_revision: revision,
      fixture: {
        files: 1,
        source_lines_per_side: SOURCE_LINES,
        change_every: CHANGE_EVERY,
        changed_lines: EXPECTED_CHANGED_LINES,
        expected_hunks: EXPECTED_CHANGED_LINES,
        actual_hunks: actualHunks,
        diff_lines: diffLines,
      },
    }, null, 2));
  } else {
    browser = await chromium.launch({ headless: true });
    const discovery = await newLayoutPage(browser, 'split');
    let serverOutput = '';
    const serverStart = performance.now();
    server = spawn(binary, ['_serve', '--no-open', '--port', String(port)], {
      cwd: fixtureDir,
      env: { ...process.env, HOME: fakeHome },
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    server.stdout.on('data', (chunk) => { serverOutput += chunk; });
    server.stderr.on('data', (chunk) => { serverOutput += chunk; });
    const earlyExit = new Promise((_, rejectExit) => {
      server.once('exit', (code, signal) => {
        rejectExit(new Error(`crit exited before measurement (${signal || code})\n${serverOutput}`));
      });
    });

    const listeningPromise = poll(origin, () => true).then(() => performance.now() - serverStart);
    const readyPromise = poll(`${origin}/api/session`, (response) => response.ok)
      .then(() => performance.now() - serverStart);
    const serverListeningMs = await Promise.race([listeningPromise, earlyExit]);
    const sessionReadyMs = await Promise.race([readyPromise, earlyExit]);

    const diffResponse = await fetch(`${origin}/api/file/diff?path=synthetic-large.ts`);
    if (!diffResponse.ok) throw new Error(`Diff endpoint returned ${diffResponse.status}`);
    const diff = await diffResponse.json();
    const actualHunks = Array.isArray(diff.hunks) ? diff.hunks.length : 0;
    const diffLines = Array.isArray(diff.hunks)
      ? diff.hunks.reduce((sum, hunk) => sum + (hunk.Lines || []).length, 0)
      : 0;

    const fixtureResult = {
      files: 1,
      source_lines_per_side: SOURCE_LINES,
      change_every: CHANGE_EVERY,
      changed_lines: EXPECTED_CHANGED_LINES,
      expected_hunks: EXPECTED_CHANGED_LINES,
      actual_hunks: actualHunks,
      diff_lines: diffLines,
    };

    const navigationStart = performance.now();
    await discovery.page.goto(origin, { waitUntil: 'domcontentloaded', timeout: 30_000 });
    await discovery.page.locator('.tree-file').first().waitFor({ state: 'visible', timeout: 120_000 });
    const startToFirstClickableRowMs = performance.now() - serverStart;
    const splitGateMs = await waitForLargeDiffGate(discovery.page);

    const measurements = {};
    measurements.split = {
      navigation_to_large_diff_gate_ms: splitGateMs,
      ...(await measureMount(discovery.page, 'split')),
      scroll: await measureScroll(discovery.page),
    };
    await discovery.context.close();

    const unified = await newLayoutPage(browser, 'unified');
    await unified.page.goto(origin, { waitUntil: 'domcontentloaded', timeout: 30_000 });
    const unifiedGateMs = await waitForLargeDiffGate(unified.page);
    measurements.unified = {
      navigation_to_large_diff_gate_ms: unifiedGateMs,
      ...(await measureMount(unified.page, 'unified')),
      scroll: await measureScroll(unified.page),
    };
    await unified.context.close();

    console.log(JSON.stringify({
      warning: 'Throwaway local benchmark; compare runs on the same machine and Chromium build.',
      measured_at: new Date().toISOString(),
      crit_revision: revision,
      chromium: browser.version(),
      node: process.version,
      machine: {
        platform: platform(),
        arch: arch(),
        cpu: cpus()[0]?.model || 'unknown',
      },
      viewport: { width: 1440, height: 900 },
      fixture: fixtureResult,
      discovery: {
        server_listening_ms: serverListeningMs,
        session_ready_ms: sessionReadyMs,
        start_to_first_clickable_row_ms: startToFirstClickableRowMs,
        browser_navigation_started_ms_after_spawn: navigationStart - serverStart,
      },
      measurements,
    }, null, 2));
  }
} finally {
  await browser?.close();
  await stopServer(server);
  if (tempRoot) await rm(tempRoot, { recursive: true, force: true });
}
