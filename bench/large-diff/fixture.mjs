// THROWAWAY SPIKE: deterministic input matching spikes/pierre-diffs/fixture.js.
export const SOURCE_LINES = 10_000;
export const CHANGE_EVERY = 20;
export const EXPECTED_CHANGED_LINES = SOURCE_LINES / CHANGE_EVERY;

function sourceLine(lineNumber, changed) {
  const serial = String(lineNumber).padStart(5, '0');
  const label = changed ? `changed-${serial}` : `stable-${serial}`;
  const active = changed ? 'false' : 'true';
  return `export const row${serial} = { id: ${lineNumber}, label: "${label}", active: ${active} };`;
}

export function makeLargeFixture() {
  const oldLines = new Array(SOURCE_LINES);
  const newLines = new Array(SOURCE_LINES);

  for (let index = 0; index < SOURCE_LINES; index += 1) {
    const lineNumber = index + 1;
    oldLines[index] = sourceLine(lineNumber, false);
    newLines[index] = sourceLine(lineNumber, lineNumber % CHANGE_EVERY === 0);
  }

  return {
    oldContents: oldLines.join('\n') + '\n',
    newContents: newLines.join('\n') + '\n',
  };
}
