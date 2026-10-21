'use strict';
// Unit tests for crit-subthreads.js. No jsdom: the DOM-touching helpers get
// minimal shims for exactly what they read (closest, classList, dataset,
// Selection/Range, TreeWalker).
const { test } = require('node:test');
const assert = require('node:assert/strict');

const st = require('../crit-subthreads.js');

// ----- Text normalization -----

test('normalizeText collapses whitespace runs and drops leading whitespace', () => {
  assert.equal(st.normalizeText('  a \n\n b\tc  '), 'a b c ');
  assert.equal(st.normalizeText(''), '');
  assert.equal(st.normalizeText(null), '');
});

test('normalizeWithMap maps each normalized character to its raw index', () => {
  const { text, map } = st.normalizeWithMap('\n a  b\nc');
  assert.equal(text, 'a b c');
  // a=2, collapsed run "  " starts at 3, b=5, "\n"=6, c=7, then the raw length.
  assert.deepEqual(map, [2, 3, 5, 6, 7, 8]);
  assert.equal(text, st.normalizeText('\n a  b\nc'));
});

// ----- Quote and offset -----

test('quoteFromParts trims the quote and counts the offset on normalized text', () => {
  assert.deepEqual(st.quoteFromParts('Hello ', 'world'), { quote: 'world', quoteOffset: 6 });
  // Leading whitespace in the selection belongs before the quote.
  assert.deepEqual(st.quoteFromParts('Hello', ' world '), { quote: 'world', quoteOffset: 6 });
  // Whitespace runs and leading body whitespace count like the normalized text.
  assert.deepEqual(st.quoteFromParts('\n  First\n\nline ', 'second'), { quote: 'second', quoteOffset: 11 });
  assert.equal(st.normalizeText('\n  First\n\nline second').slice(11, 17), 'second');
  // Inner line breaks stay in the quote.
  assert.equal(st.quoteFromParts('', 'a\n\nb').quote, 'a\n\nb');
});

test('quoteFromParts returns null for an empty or whitespace-only selection', () => {
  assert.equal(st.quoteFromParts('abc', ''), null);
  assert.equal(st.quoteFromParts('abc', ' \n '), null);
});

test('locateQuote uses the offset first, then the nearest exact match', () => {
  const raw = 'the cat and the cat';
  // Offset 16 still holds the quote: that occurrence wins over the first.
  assert.deepEqual(st.locateQuote(raw, 'cat', 16), { start: 16, end: 19 });
  // A stale offset picks the nearest occurrence.
  assert.deepEqual(st.locateQuote(raw, 'cat', 14), { start: 16, end: 19 });
  assert.deepEqual(st.locateQuote(raw, 'cat', 2), { start: 4, end: 7 });
  // No offset: the first occurrence.
  assert.deepEqual(st.locateQuote(raw, 'cat'), { start: 4, end: 7 });
});

test('locateQuote falls back to a case-insensitive match', () => {
  assert.deepEqual(st.locateQuote('Use The Helper', 'the helper', 0), { start: 4, end: 14 });
});

test('locateQuote matches across different whitespace and returns raw positions', () => {
  const raw = '<p>\n  First   paragraph</p>\n<p>Second\nline</p>'.replace(/<[^>]+>/g, '');
  const hit = st.locateQuote(raw, 'paragraph\n\nSecond line', null);
  assert.ok(hit);
  assert.equal(raw.slice(hit.start, hit.end), 'paragraph\nSecond\nline');
});

test('locateQuote returns null when the passage is gone', () => {
  assert.equal(st.locateQuote('nothing here', 'missing', 0), null);
  assert.equal(st.locateQuote('text', '   ', 0), null);
  assert.equal(st.locateQuote('text', '', 0), null);
});

// ----- Selection → message (DOM shims) -----
//
// A small tree of elements and text nodes. Boundary points map to positions
// in the concatenated text, which is what Range.toString reads.

function matchesSimple(el, part) {
  const m = /^\s*\.([\w-]+)(?:\[data-([\w-]+)\])?\s*$/.exec(part);
  if (!m || !el.classes.includes(m[1])) return false;
  if (!m[2]) return true;
  const key = m[2].replace(/-(\w)/g, (_, ch) => ch.toUpperCase());
  return Object.prototype.hasOwnProperty.call(el.dataset, key);
}

function el(classes, data, children) {
  const node = {
    nodeType: 1,
    classes,
    classList: { contains: (c) => classes.includes(c) },
    dataset: data || {},
    parentElement: null,
    childNodes: children || [],
    matches(selector) { return selector.split(',').some((part) => matchesSimple(this, part)); },
    closest(selector) {
      for (let cur = this; cur; cur = cur.parentElement) {
        if (cur.matches(selector)) return cur;
      }
      return null;
    },
  };
  node.childNodes.forEach((child) => { child.parentElement = node; });
  return node;
}

function txt(data) {
  return { nodeType: 3, data, parentElement: null };
}

// Text positions of every node of `root`, in document order.
function layout(root) {
  const spans = new Map();
  let text = '';
  (function walk(node) {
    const start = text.length;
    if (node.nodeType === 3) text += node.data;
    else node.childNodes.forEach(walk);
    spans.set(node, { start, end: text.length });
  })(root);
  const pos = (node, offset) => {
    if (node.nodeType === 3) return spans.get(node).start + offset;
    return offset < node.childNodes.length ? spans.get(node.childNodes[offset]).start : spans.get(node).end;
  };
  const doc = {
    createRange() {
      let start = 0;
      let end = 0;
      return {
        setStart(node, offset) { start = pos(node, offset); },
        setEnd(node, offset) { end = pos(node, offset); },
        toString() { return end > start ? text.slice(start, end) : ''; },
      };
    },
  };
  const select = (startNode, startOffset, endNode, endOffset) => {
    const range = doc.createRange();
    range.setStart(startNode, startOffset);
    range.setEnd(endNode, endOffset);
    Object.assign(range, {
      startContainer: startNode, startOffset, endContainer: endNode, endOffset,
      collapsed: pos(startNode, startOffset) === pos(endNode, endOffset),
    });
    return { isCollapsed: range.collapsed, rangeCount: 1, getRangeAt: () => range };
  };
  return { doc, select };
}

// A thread card: header, comment body, the body's sub-threads, two replies
// and a reply box; then a document line outside the card.
function makeThread() {
  const t = {};
  t.headerText = txt('@me 12:00');
  t.bodyText = txt('The quick brown fox jumps.');
  t.body = el(['comment-body'], {}, [el(['p'], {}, [t.bodyText]), txt('\n')]);
  t.subthreads = el(['subthreads'], {}, [el(['comment-block'], {}, [txt('nested sub-thread')])]);
  t.replyText = txt('Option A or option B.');
  t.replyFirstText = txt('First paragraph.');
  t.replyBody = el(['reply-body'], {}, [
    el(['p'], {}, [t.replyFirstText]), txt('\n'), el(['p'], {}, [t.replyText]), txt('\n'),
  ]);
  t.reply = el(['comment-reply'], { replyId: 'rp_rep001' }, [el(['reply-header'], {}, [txt('@agent 12:01')]), t.replyBody]);
  t.reply2HeaderText = txt('@agent 12:02');
  t.reply2 = el(['comment-reply'], { replyId: 'rp_rep002' }, [
    el(['reply-header'], {}, [t.reply2HeaderText]), el(['reply-body'], {}, [txt('Second reply.')]),
  ]);
  t.replyForm = el(['reply-form'], {}, [txt('')]);
  t.card = el(['comment-card'], { commentId: 'c_root01' }, [
    el(['comment-header'], {}, [t.headerText]), t.body, t.subthreads, el(['comment-replies'], {}, [t.reply, t.reply2]), t.replyForm,
  ]);
  t.lineText = txt('some code');
  t.line = el(['line-content'], {}, [t.lineText]);
  t.root = el(['root'], {}, [t.card, t.line]);
  Object.assign(t, layout(t.root));
  return t;
}

test('resolveMessageSelection resolves a passage of a comment body to the card', () => {
  const t = makeThread();
  assert.deepEqual(st.resolveMessageSelection(t.select(t.bodyText, 4, t.bodyText, 15), t.doc),
    { parentId: 'c_root01', quote: 'quick brown', quoteOffset: 4 });
});

test('resolveMessageSelection resolves a passage of a reply body to the reply', () => {
  const t = makeThread();
  // "First paragraph.\n" comes before: normalized, 17 characters.
  assert.deepEqual(st.resolveMessageSelection(t.select(t.replyText, 12, t.replyText, 20), t.doc),
    { parentId: 'rp_rep001', quote: 'option B', quoteOffset: 29 });
});

test('resolveMessageSelection pulls back an end that overshoots the body by whitespace only', () => {
  const t = makeThread();
  // Triple-click on a reply's last paragraph: the end lands on the next reply.
  assert.deepEqual(st.resolveMessageSelection(t.select(t.replyText, 0, t.reply2, 0), t.doc),
    { parentId: 'rp_rep001', quote: 'Option A or option B.', quoteOffset: 17 });
  // A drag past a comment body's last line: the end lands on its sub-threads.
  assert.deepEqual(st.resolveMessageSelection(t.select(t.bodyText, 4, t.subthreads, 0), t.doc),
    { parentId: 'c_root01', quote: 'quick brown fox jumps.', quoteOffset: 4 });
});

test('resolveMessageSelection pulls forward a start just before the body', () => {
  const t = makeThread();
  assert.deepEqual(st.resolveMessageSelection(t.select(t.headerText, t.headerText.data.length, t.bodyText, 9), t.doc),
    { parentId: 'c_root01', quote: 'The quick', quoteOffset: 0 });
});

test('resolveMessageSelection rejects selections that take text outside one message body', () => {
  const t = makeThread();
  // Into the next reply's header.
  assert.equal(st.resolveMessageSelection(t.select(t.replyText, 0, t.reply2HeaderText, 3), t.doc), null);
  // From a comment body into a reply body.
  assert.equal(st.resolveMessageSelection(t.select(t.bodyText, 4, t.replyText, 5), t.doc), null);
  // Over the nested sub-thread under the body.
  assert.equal(st.resolveMessageSelection(t.select(t.bodyText, 4, t.replyFirstText, 3), t.doc), null);
  // From a message onto the document.
  assert.equal(st.resolveMessageSelection(t.select(t.replyText, 0, t.lineText, 4), t.doc), null);
  // From the header into the body.
  assert.equal(st.resolveMessageSelection(t.select(t.headerText, 1, t.bodyText, 9), t.doc), null);
  // A document line alone.
  assert.equal(st.resolveMessageSelection(t.select(t.lineText, 0, t.lineText, 4), t.doc), null);
});

test('resolveMessageSelection ignores empty and whitespace-only selections', () => {
  const t = makeThread();
  assert.equal(st.resolveMessageSelection(null, t.doc), null);
  assert.equal(st.resolveMessageSelection({ isCollapsed: true, rangeCount: 1, getRangeAt: () => null }, t.doc), null);
  assert.equal(st.resolveMessageSelection({ isCollapsed: false, rangeCount: 0 }, t.doc), null);
  assert.equal(st.resolveMessageSelection(t.select(t.bodyText, 3, t.bodyText, 4), t.doc), null);
});

test('resolveMessageSelection needs the message id on the card or reply', () => {
  const text = txt('no id here');
  const card = el(['comment-card'], {}, [el(['comment-body'], {}, [text])]);
  const { doc, select } = layout(el(['root'], {}, [card]));
  assert.equal(st.resolveMessageSelection(select(text, 0, text, 5), doc), null);
});

// ----- Quote → DOM range -----

test('quoteRange spans the text nodes holding the quote', () => {
  const nodes = [{ data: 'Use ' }, { data: 'the helper' }, { data: ' here.' }];
  const fakeDoc = {
    createTreeWalker() {
      let i = 0;
      return { nextNode: () => nodes[i++] || null };
    },
    createRange() {
      const r = {};
      return {
        setStart(node, offset) { r.startContainer = node; r.startOffset = offset; },
        setEnd(node, offset) { r.endContainer = node; r.endOffset = offset; },
        get startContainer() { return r.startContainer; },
        get startOffset() { return r.startOffset; },
        get endContainer() { return r.endContainer; },
        get endOffset() { return r.endOffset; },
      };
    },
  };
  const range = st.quoteRange({}, 'helper here', 8, fakeDoc);
  assert.equal(range.startContainer, nodes[1]);
  assert.equal(range.startOffset, 4);
  assert.equal(range.endContainer, nodes[2]);
  assert.equal(range.endOffset, 5);
  assert.equal(st.quoteRange({}, 'absent', 0, fakeDoc), null);
});

// ----- Comment lists -----

const list = [
  { id: 'c_root01', body: 'root', replies: [{ id: 'rp_a' }, { id: 'rp_b' }] },
  { id: 'c_sub002', parent_id: 'rp_a', created_at: '2026-10-16T10:00:02Z', replies: [{ id: 'rp_c' }] },
  { id: 'c_sub001', parent_id: 'rp_a', created_at: '2026-10-16T10:00:01Z', resolved: true },
  { id: 'c_sub003', parent_id: 'c_root01', created_at: '2026-10-16T10:00:03Z', resolved: true },
  { id: 'c_deep01', parent_id: 'rp_c', created_at: '2026-10-16T10:00:04Z' },
  { id: 'c_root02', body: 'other root' },
];

test('partitionComments separates root threads from sub-threads', () => {
  const parts = st.partitionComments(list);
  assert.deepEqual(parts.roots.map((c) => c.id), ['c_root01', 'c_root02']);
  assert.deepEqual(parts.subThreads.map((c) => c.id), ['c_sub002', 'c_sub001', 'c_sub003', 'c_deep01']);
  assert.deepEqual(st.partitionComments(null), { roots: [], subThreads: [] });
});

test('indexByParent groups sub-threads by parent, oldest first', () => {
  const index = st.indexByParent(st.partitionComments(list).subThreads);
  assert.deepEqual(index.get('rp_a').map((c) => c.id), ['c_sub001', 'c_sub002']);
  assert.deepEqual(index.get('c_root01').map((c) => c.id), ['c_sub003']);
  assert.equal(index.get('c_root02'), undefined);
});

test('rootIdOf walks sub-threads and replies up to the root thread', () => {
  assert.equal(st.rootIdOf('c_deep01', list), 'c_root01');
  assert.equal(st.rootIdOf('rp_c', list), 'c_root01');
  assert.equal(st.rootIdOf('c_root02', list), 'c_root02');
  assert.equal(st.rootIdOf('c_missing', list), null);
});

test('rootIdOf stops on a broken or cyclic chain', () => {
  assert.equal(st.rootIdOf('c_x', [{ id: 'c_x', parent_id: 'rp_gone' }]), null);
  assert.equal(st.rootIdOf('c_a', [{ id: 'c_a', parent_id: 'c_b' }, { id: 'c_b', parent_id: 'c_a' }]), null);
});

test('descendants cover replies and every depth', () => {
  const index = st.indexByParent(st.partitionComments(list).subThreads);
  // The root: its body's sub-thread, both sub-threads on rp_a and the one on
  // the reply of a sub-thread.
  assert.deepEqual(st.descendants(list[0], index).map((c) => c.id).sort(),
    ['c_deep01', 'c_sub001', 'c_sub002', 'c_sub003']);
  // A reply: only what hangs under it.
  assert.equal(st.descendants({ id: 'rp_a' }, index).length, 3);
  assert.equal(st.descendants({ id: 'rp_b' }, index).length, 0);
  assert.equal(st.descendants(list[5], index).length, 0);
});

test('descendants survives a cycle', () => {
  const cyclic = [{ id: 'c_a', parent_id: 'c_b' }, { id: 'c_b', parent_id: 'c_a' }];
  assert.equal(st.descendants({ id: 'c_a' }, st.indexByParent(cyclic)).length, 2);
});

test('hasOpenDescendant finds an unresolved sub-thread at any depth', () => {
  const index = st.indexByParent(st.partitionComments(list).subThreads);
  assert.equal(st.hasOpenDescendant(list[0], index), true);
  // Under the root body alone, only a resolved sub-thread.
  assert.equal(st.hasOpenDescendant({ id: 'c_root01' }, index), false);
  assert.equal(st.hasOpenDescendant({ id: 'rp_c' }, index), true);
});

test('messageIds holds every comment, sub-thread and reply id of a list', () => {
  assert.deepEqual([...st.messageIds(list)].sort(), [
    'c_deep01', 'c_root01', 'c_root02', 'c_sub001', 'c_sub002', 'c_sub003', 'rp_a', 'rp_b', 'rp_c',
  ]);
  assert.equal(st.messageIds(null).size, 0);
});

test('orphanSubThreads lists sub-threads whose parent is missing', () => {
  const parts = st.partitionComments(list.concat([{ id: 'c_orph01', parent_id: 'rp_deleted' }]));
  assert.deepEqual(st.orphanSubThreads(parts.roots, parts.subThreads).map((c) => c.id), ['c_orph01']);
});
