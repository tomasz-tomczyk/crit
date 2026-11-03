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
    { filePath: 'plan.md', startLine: 1, endLine: 1, text: 'Plan', offset: 0 },
    { filePath: 'plan.md', startLine: 3, endLine: 3, text: 'a b', offset: 0 },
    { filePath: 'plan.md', startLine: 5, endLine: 6, text: 'Two lines', offset: 0 },
  ]);
});

test('segmentsFromBlocks splits a block into sentences with their offsets', () => {
  const segments = readAloud.segmentsFromBlocks('plan.md', [
    { startLine: 1, endLine: 2, html: '<p>First one. Second, e.g. this one!  "Third?" Done</p>' },
  ], strip);
  assert.deepEqual(segments.map((s) => [s.text, s.offset]), [
    ['First one.', 0],
    ['Second, e.g. this one!', 11],
    ['"Third?"', 34],
    ['Done', 43],
  ]);
  const text = 'First one. Second, e.g. this one! "Third?" Done';
  for (const s of segments) assert.equal(text.slice(s.offset, s.offset + s.text.length), s.text);
});

test('segmentsFromBlocks cuts a long sentence at clause breaks', () => {
  const clause = 'word '.repeat(10).trim();
  const sentence = [clause, clause, clause, clause].join(', ') + '.';
  const segments = readAloud.segmentsFromBlocks('plan.md', [{ startLine: 1, endLine: 1, html: sentence }], strip);
  assert.ok(segments.length > 1);
  for (const s of segments) {
    assert.ok(s.text.length <= 160);
    assert.equal(sentence.slice(s.offset, s.offset + s.text.length), s.text);
  }
  assert.equal(segments.map((s) => s.text).join(' '), sentence);
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

test('positionAt finds the sentence and word for a click offset in a block', () => {
  const segments = [
    { filePath: 'a.md', startLine: 1, endLine: 1, text: 'Hi.', offset: 0 },
    { filePath: 'b.md', startLine: 1, endLine: 1, text: 'One two.', offset: 0 },
    { filePath: 'b.md', startLine: 1, endLine: 1, text: 'Three four five.', offset: 9 },
    { filePath: 'b.md', startLine: 3, endLine: 3, text: 'Code block.', offset: 0 },
    { filePath: 'b.md', startLine: 7, endLine: 8, text: 'End.', offset: 0 },
  ];
  assert.deepEqual(readAloud.positionAt(segments, 'b.md', 1, 0), { index: 1, charIndex: 0 });
  assert.deepEqual(readAloud.positionAt(segments, 'b.md', 1, 6), { index: 1, charIndex: 4 });
  assert.deepEqual(readAloud.positionAt(segments, 'b.md', 1, 17), { index: 2, charIndex: 6 });
  // A line inside skipped code lands on the announcement.
  assert.deepEqual(readAloud.positionAt(segments, 'b.md', 5, 3), { index: 3, charIndex: 0 });
  assert.deepEqual(readAloud.positionAt(segments, 'b.md', 8, 0), { index: 4, charIndex: 0 });
  assert.deepEqual(readAloud.positionAt(segments, 'c.md', 4, 0), { index: 0, charIndex: 0 });
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

test('createReader pause resumes from the word being spoken', () => {
  const { synth, Utterance } = fakeSynth();
  const reader = readAloud.createReader({ synth, Utterance });
  reader.start([{ text: 'Alpha beta gamma delta.' }, { text: 'Next.' }], 0);
  synth.spoken[0].onboundary({ name: 'word', charIndex: 6 });
  synth.spoken[0].onboundary({ name: 'word', charIndex: 11 });
  reader.pause();
  assert.equal(reader.charIndex(), 11);
  reader.resume();
  assert.equal(synth.spoken[1].text, 'gamma delta.');
  // Boundaries of the resumed utterance count from where it resumed.
  synth.spoken[1].onboundary({ name: 'word', charIndex: 6 });
  reader.pause();
  reader.resume();
  assert.equal(synth.spoken[2].text, 'delta.');
  synth.spoken[2].onend();
  assert.equal(synth.spoken[3].text, 'Next.');
});

test('createReader ignores boundaries of a cancelled utterance and sentence boundaries', () => {
  const { synth, Utterance } = fakeSynth();
  const reader = readAloud.createReader({ synth, Utterance });
  reader.start([{ text: 'Alpha beta gamma.' }], 0);
  const first = synth.spoken[0];
  first.onboundary({ name: 'sentence', charIndex: 11 });
  reader.setRate(2);
  first.onboundary({ name: 'word', charIndex: 11 });
  reader.pause();
  reader.resume();
  assert.equal(synth.spoken[synth.spoken.length - 1].text, 'Alpha beta gamma.');
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

test('createReader seek can start inside a segment', () => {
  const { synth, Utterance } = fakeSynth();
  const reader = readAloud.createReader({ synth, Utterance });
  reader.start([{ text: 'a' }, { text: 'One two three.' }], 0);
  reader.seek(1, 4);
  assert.equal(synth.spoken[synth.spoken.length - 1].text, 'two three.');
  reader.pause();
  reader.seek(0, 0);
  reader.seek(1, 8);
  reader.resume();
  assert.equal(synth.spoken[synth.spoken.length - 1].text, 'three.');
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

test('createReader rate change continues from the current word', () => {
  const { synth, Utterance } = fakeSynth();
  const reader = readAloud.createReader({ synth, Utterance });
  reader.start([{ text: 'One two three.' }], 0);
  synth.spoken[0].onboundary({ name: 'word', charIndex: 4 });
  reader.setRate(1.5);
  const last = synth.spoken[synth.spoken.length - 1];
  assert.equal(last.text, 'two three.');
  assert.equal(last.rate, 1.5);
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
