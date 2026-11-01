const test = require('node:test');
const assert = require('node:assert/strict');
const readAloud = require('../crit-read-aloud.js');

const strip = (html) => html.replace(/<[^>]*>/g, '');

test('segmentsFromBlocks reads text blocks and skips empty lines and table separators', () => {
  const segments = readAloud.segmentsFromBlocks('plan.md', [
    { startLine: 1, endLine: 1, html: '<h1>Plan</h1>' },
    { startLine: 2, endLine: 2, html: '', isEmpty: true },
    { startLine: 3, endLine: 3, html: '<tr><td>a</td><td>b</td></tr>' },
    { startLine: 4, endLine: 4, html: '', cssClass: 'table-separator' },
    { startLine: 5, endLine: 6, html: '<p>Two\n  lines</p>' },
  ], strip);
  assert.deepEqual(segments, [
    { filePath: 'plan.md', startLine: 1, endLine: 1, text: 'Plan' },
    { filePath: 'plan.md', startLine: 3, endLine: 3, text: 'a b' },
    { filePath: 'plan.md', startLine: 5, endLine: 6, text: 'Two lines' },
  ]);
});

test('segmentsFromBlocks announces code and diagrams once instead of reading them', () => {
  const segments = readAloud.segmentsFromBlocks('plan.md', [
    { startLine: 1, endLine: 1, html: '<span class="fence-marker">```go</span>', cssClass: 'code-line code-first' },
    { startLine: 2, endLine: 2, html: '<code class="crit-code">x := 1</code>', cssClass: 'code-line' },
    { startLine: 3, endLine: 3, html: '<span class="fence-marker">```</span>', cssClass: 'code-line code-last' },
    { startLine: 4, endLine: 6, html: '<div>graph</div>', cssClass: 'render-block' },
  ], strip);
  assert.deepEqual(segments.map((s) => s.text), ['Code block.', 'Diagram.']);
});

test('startIndexFor finds the first segment at or after the line in that file', () => {
  const segments = [
    { filePath: 'a.md', startLine: 1, endLine: 1 },
    { filePath: 'b.md', startLine: 1, endLine: 2 },
    { filePath: 'b.md', startLine: 5, endLine: 5 },
  ];
  assert.equal(readAloud.startIndexFor(segments, 'b.md', 3), 2);
  assert.equal(readAloud.startIndexFor(segments, 'b.md', 1), 1);
  assert.equal(readAloud.startIndexFor(segments, 'c.md', 1), 0);
});

test('indexAtLine finds the segment covering a line, including lines inside skipped code', () => {
  const segments = [
    { filePath: 'a.md', startLine: 1, endLine: 1 },
    { filePath: 'b.md', startLine: 1, endLine: 1 },
    { filePath: 'b.md', startLine: 3, endLine: 3 },
    { filePath: 'b.md', startLine: 7, endLine: 8 },
  ];
  assert.equal(readAloud.indexAtLine(segments, 'b.md', 5), 2);
  assert.equal(readAloud.indexAtLine(segments, 'b.md', 8), 3);
  assert.equal(readAloud.indexAtLine(segments, 'b.md', 1), 1);
  assert.equal(readAloud.indexAtLine(segments, 'c.md', 4), 0);
});

function fakeSynth() {
  const synth = {
    spoken: [],
    cancelled: 0,
    speak(u) { synth.spoken.push(u); },
    cancel() { synth.cancelled++; const last = synth.spoken[synth.spoken.length - 1]; if (last && last.onerror) last.onerror({ error: 'interrupted' }); },
  };
  function Utterance(text) { this.text = text; }
  return { synth, Utterance };
}

test('createReader speaks segments in order from the start index and reports state', () => {
  const { synth, Utterance } = fakeSynth();
  const seen = [];
  const states = [];
  const reader = readAloud.createReader({
    synth, Utterance,
    onSegment: (s, i) => seen.push(i),
    onStateChange: (st) => states.push(st),
  });
  reader.start([{ text: 'a' }, { text: 'b' }, { text: 'c' }], 1);
  assert.equal(reader.state(), 'playing');
  assert.equal(synth.spoken[0].text, 'b');
  synth.spoken[0].onend();
  assert.equal(synth.spoken[1].text, 'c');
  synth.spoken[1].onend();
  assert.equal(reader.state(), 'stopped');
  assert.deepEqual(seen, [1, 2]);
  assert.deepEqual(states, ['playing', 'stopped']);
});

test('createReader stop ignores events from the cancelled utterance', () => {
  const { synth, Utterance } = fakeSynth();
  const seen = [];
  const reader = readAloud.createReader({ synth, Utterance, onSegment: (s, i) => seen.push(i) });
  reader.start([{ text: 'a' }, { text: 'b' }], 0);
  const first = synth.spoken[0];
  reader.stop();
  first.onend();
  assert.equal(reader.state(), 'stopped');
  assert.equal(synth.spoken.length, 1);
  assert.deepEqual(seen, [0]);
});

test('createReader stops on a speech error', () => {
  const { synth, Utterance } = fakeSynth();
  const reader = readAloud.createReader({ synth, Utterance });
  reader.start([{ text: 'a' }, { text: 'b' }], 0);
  synth.spoken[0].onerror({ error: 'synthesis-failed' });
  assert.equal(reader.state(), 'stopped');
  assert.equal(synth.spoken.length, 1);
});

test('createReader pause stops speaking and resume restarts the current block', () => {
  const { synth, Utterance } = fakeSynth();
  const reader = readAloud.createReader({ synth, Utterance });
  reader.start([{ text: 'a' }, { text: 'b' }], 1);
  reader.pause();
  assert.equal(reader.state(), 'paused');
  synth.spoken[0].onend();
  assert.equal(synth.spoken.length, 1);
  reader.resume();
  assert.equal(reader.state(), 'playing');
  assert.equal(synth.spoken[1].text, 'b');
});

test('createReader seek jumps to a block, clamped to the document', () => {
  const { synth, Utterance } = fakeSynth();
  const reader = readAloud.createReader({ synth, Utterance });
  reader.start([{ text: 'a' }, { text: 'b' }, { text: 'c' }], 0);
  reader.seek(2);
  assert.equal(synth.spoken[synth.spoken.length - 1].text, 'c');
  reader.seek(-5);
  assert.equal(reader.index(), 0);
  assert.equal(synth.spoken[synth.spoken.length - 1].text, 'a');
  reader.seek(99);
  assert.equal(reader.index(), 2);
});

test('createReader seek while paused moves without speaking', () => {
  const { synth, Utterance } = fakeSynth();
  const seen = [];
  const reader = readAloud.createReader({ synth, Utterance, onSegment: (s, i) => seen.push(i) });
  reader.start([{ text: 'a' }, { text: 'b' }], 0);
  reader.pause();
  reader.seek(1);
  assert.equal(synth.spoken.length, 1);
  assert.deepEqual(seen, [0, 1]);
  reader.resume();
  assert.equal(synth.spoken[1].text, 'b');
});

test('createReader applies rate and volume to utterances and restarts the block on change', () => {
  const { synth, Utterance } = fakeSynth();
  const reader = readAloud.createReader({ synth, Utterance, rate: 1.5, volume: 0.4 });
  reader.start([{ text: 'a' }, { text: 'b' }], 0);
  assert.equal(synth.spoken[0].rate, 1.5);
  assert.equal(synth.spoken[0].volume, 0.4);
  reader.setRate(2);
  reader.setVolume(5);
  const last = synth.spoken[synth.spoken.length - 1];
  assert.equal(last.text, 'a');
  assert.equal(last.rate, 2);
  assert.equal(last.volume, 1);
});

test('isSupported needs both speechSynthesis and SpeechSynthesisUtterance', () => {
  assert.equal(readAloud.isSupported({ speechSynthesis: {}, SpeechSynthesisUtterance: function () {} }), true);
  assert.equal(readAloud.isSupported({ speechSynthesis: {} }), false);
  assert.equal(readAloud.isSupported(undefined), false);
});
