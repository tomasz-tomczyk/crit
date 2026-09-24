# THROWAWAY: Crit current-renderer large-diff measurement

This harness measures Crit's production embedded UI with the same synthetic
fixture shape as the Pierre spike:

- one 10,000-line TypeScript file on each side;
- every 20th line changed (500 edits / expected 500 separate hunks);
- split and unified layouts;
- 1440×900 headless Chromium;
- 180 animation-frame steps from the top to the bottom of the rendered page.

It deliberately creates a disposable git repository and launches a real
`crit _serve`. Crit's diff render functions are private and tightly coupled to
application state, so copying them into a minimal page would not measure the
current renderer honestly. Fixture creation and `go build` happen before the
measurement clock starts.

## Run

The script reuses the E2E suite's pinned Playwright installation:

```bash
cd test/e2e
npm ci
npx playwright install chromium
cd ../..
node bench/large-diff/measure.mjs
```

Set `CRIT_MEASURE_PORT` if port 4179 is occupied. The script prints one JSON
object, cleans up its temporary repo/home/binary, and terminates the daemon.

If Chromium or local port binding is unavailable in a restricted environment,
validate the generated git fixture without claiming server/browser numbers:

```bash
node bench/large-diff/measure.mjs --validate-only
```

For a same-machine Pierre comparison, run its `npm run measure` separately and
compare Pierre `firstPaintMs` with Crit `file_body_first_paint_ms`, plus the
three scroll fields. Do not compare Pierre's mount clock with Crit's discovery
or `navigation_to_large_diff_gate_ms`: Crit's gate time includes production API
fetches and pre-highlighting, while Pierre reports fixture generation/parsing
separately.

This is a local diagnostic, not a stable CI performance test. Close expensive
background workloads, retain the emitted revision/Chromium/machine metadata,
and compare runs made on the same hardware.
