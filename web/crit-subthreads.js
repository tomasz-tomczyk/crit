// crit-subthreads.js — sub-threads: a thread that comments on a passage of
// another message (a comment body or a reply body) and is shown nested under
// that message. Mounted on window.crit.subthreads.
//
// A sub-thread is a comment with a `parent_id`: the id of the commented
// comment (root thread or sub-thread) or reply. It lives in the same list as
// its root thread (a file's comments, or the review-level comments). Its
// passage is `quote` + `quote_offset`, where the offset is the start of the
// passage in the parent's rendered body text, whitespace-normalized (runs of
// whitespace count as one space, leading whitespace is dropped) — the same
// convention as document quotes.
//
// Reads no window.crit namespace. Everything here is pure except
// resolveMessageSelection (Selection, Range, Element.closest, createRange)
// and quoteRange (TreeWalker, StaticRange/Range), which only touch the nodes
// they are given.
(function () {
  'use strict';

  // Bodies that can be commented: a card's own body, or a reply's body.
  var MESSAGE_BODY_SELECTOR = '.comment-body, .reply-body';
  var SHOW_TEXT = 4; // NodeFilter.SHOW_TEXT

  // ----- Text normalization -----

  // Collapse whitespace runs to one space and drop leading whitespace.
  function normalizeText(s) {
    return String(s || '').replace(/\s+/g, ' ').trimStart();
  }

  // normalizeText plus, for each normalized character, the raw index it came
  // from (a collapsed run maps to its first character). map has one extra
  // entry, the raw length, so an end position always maps.
  function normalizeWithMap(raw) {
    raw = String(raw || '');
    var text = '';
    var map = [];
    var run = -1;
    for (var i = 0; i < raw.length; i++) {
      if (/\s/.test(raw[i])) {
        if (run < 0) run = i;
        continue;
      }
      if (run >= 0 && text.length > 0) {
        text += ' ';
        map.push(run);
      }
      run = -1;
      text += raw[i];
      map.push(i);
    }
    if (run >= 0 && text.length > 0) {
      text += ' ';
      map.push(run);
    }
    map.push(raw.length);
    return { text: text, map: map };
  }

  // The quote and its offset for a selection, given the body text before the
  // selection and the selected text (both raw, as in the DOM). The quote keeps
  // its inner line breaks; null when nothing but whitespace is selected.
  function quoteFromParts(before, selected) {
    selected = String(selected || '');
    var quote = selected.trim();
    if (!quote) return null;
    var lead = /^\s*/.exec(selected)[0];
    return { quote: quote, quoteOffset: normalizeText(String(before || '') + lead).length };
  }

  // Index of `q` in `text` (both normalized): at `offset` when it is still
  // there, else the exact occurrence nearest to `offset` (the first one when
  // there is no offset), else a case-insensitive match. -1 when absent.
  function findNormalized(text, q, offset) {
    if (!q) return -1;
    var hasOffset = typeof offset === 'number' && offset >= 0;
    if (hasOffset && text.substr(offset, q.length) === q) return offset;
    var best = -1;
    for (var at = text.indexOf(q); at >= 0; at = text.indexOf(q, at + 1)) {
      if (!hasOffset) return at;
      if (best < 0 || Math.abs(at - offset) < Math.abs(best - offset)) best = at;
    }
    if (best >= 0) return best;
    var lower = text.toLowerCase();
    var lowerQ = q.toLowerCase();
    // Lower-casing can change the length of some characters; indexes would
    // no longer line up with the text, so give up rather than misplace.
    if (lower.length !== text.length || lowerQ.length !== q.length) return -1;
    return lower.indexOf(lowerQ);
  }

  // Where `quote` sits in `raw` (a body's text): { start, end } raw indexes,
  // or null when the passage is no longer there.
  function locateQuote(raw, quote, offset) {
    var q = normalizeText(quote).trimEnd();
    if (!q) return null;
    var norm = normalizeWithMap(raw);
    var at = findNormalized(norm.text, q, offset);
    if (at < 0) return null;
    return { start: norm.map[at], end: norm.map[at + q.length - 1] + 1 };
  }

  // ----- Selection → message -----

  function elementOf(node) {
    if (!node) return null;
    return node.nodeType === 1 ? node : node.parentElement || null;
  }

  // The commentable body containing `node` and the id of its message, or
  // null. A reply body belongs to its .comment-reply, a comment body to its
  // .comment-card.
  function messageBodyOf(node) {
    var el = elementOf(node);
    if (!el || typeof el.closest !== 'function') return null;
    var body = el.closest(MESSAGE_BODY_SELECTOR);
    if (!body) return null;
    if (body.classList.contains('reply-body')) {
      var reply = body.closest('.comment-reply[data-reply-id]');
      return reply && reply.dataset.replyId ? { body: body, parentId: reply.dataset.replyId } : null;
    }
    var card = body.closest('.comment-card[data-comment-id]');
    return card && card.dataset.commentId ? { body: body, parentId: card.dataset.commentId } : null;
  }

  // Text between two boundary points, as a Range reads it.
  function textBetween(doc, startNode, startOffset, endNode, endOffset) {
    var range = doc.createRange();
    range.setStart(startNode, startOffset);
    range.setEnd(endNode, endOffset);
    return range.toString();
  }

  // A text selection inside the rendered body of one message:
  // { parentId, quote, quoteOffset }. Null when the selection is empty, or
  // when it takes text outside that body (another message, the document).
  // An end that only overshoots the body by whitespace is pulled back to the
  // body: Chromium ends a triple-click on a last paragraph, or a drag past a
  // last line, at the start of the next block (a reply header, the
  // sub-threads below, the reply box). A start just before the body is
  // pulled forward the same way.
  function resolveMessageSelection(selection, doc) {
    if (!selection || selection.isCollapsed || !selection.rangeCount) return null;
    var range = selection.getRangeAt(0);
    if (!range || range.collapsed) return null;
    doc = doc || document;
    var startNode = range.startContainer;
    var startOffset = range.startOffset;
    var endNode = range.endContainer;
    var endOffset = range.endOffset;
    var start = messageBodyOf(startNode);
    var end = messageBodyOf(endNode);
    if (start && (!end || end.body !== start.body)) {
      var bodyEnd = start.body.childNodes.length;
      if (textBetween(doc, start.body, bodyEnd, endNode, endOffset).trim()) return null;
      end = start;
      endNode = start.body;
      endOffset = bodyEnd;
    } else if (!start && end) {
      if (textBetween(doc, startNode, startOffset, end.body, 0).trim()) return null;
      start = end;
      startNode = end.body;
      startOffset = 0;
    }
    if (!start || !end) return null;
    var q = quoteFromParts(
      textBetween(doc, start.body, 0, startNode, startOffset),
      textBetween(doc, startNode, startOffset, endNode, endOffset)
    );
    if (!q) return null;
    return { parentId: start.parentId, quote: q.quote, quoteOffset: q.quoteOffset };
  }

  // ----- Quote → DOM range -----

  function boundaryAt(nodes, pos, isEnd) {
    for (var i = 0; i < nodes.length; i++) {
      var n = nodes[i];
      var len = n.node.data.length;
      if (isEnd ? (pos > n.start && pos <= n.start + len) : (pos >= n.start && pos < n.start + len)) {
        return { node: n.node, offset: pos - n.start };
      }
    }
    return null;
  }

  // A range over `quote` inside `el` (a message body), or null. A StaticRange
  // where available: it keeps pointing at the same text nodes when a
  // virtualized list detaches and re-attaches the card, where a live Range
  // would collapse.
  function quoteRange(el, quote, offset, doc) {
    doc = doc || (typeof document !== 'undefined' ? document : null);
    if (!el || !doc || !quote) return null;
    var nodes = [];
    var raw = '';
    var walker = doc.createTreeWalker(el, SHOW_TEXT);
    var node;
    while ((node = walker.nextNode())) {
      nodes.push({ node: node, start: raw.length });
      raw += node.data;
    }
    var hit = locateQuote(raw, quote, offset);
    if (!hit) return null;
    var a = boundaryAt(nodes, hit.start, false);
    var b = boundaryAt(nodes, hit.end, true);
    if (!a || !b) return null;
    if (typeof StaticRange === 'function') {
      return new StaticRange({ startContainer: a.node, startOffset: a.offset, endContainer: b.node, endOffset: b.offset });
    }
    var range = doc.createRange();
    range.setStart(a.node, a.offset);
    range.setEnd(b.node, b.offset);
    return range;
  }

  // ----- Comment lists -----

  // Split a comment list into root threads (placed on lines or the file)
  // and sub-threads (placed under a message).
  function partitionComments(list) {
    var roots = [];
    var subThreads = [];
    (Array.isArray(list) ? list : []).forEach(function (c) {
      if (!c) return;
      if (c.parent_id) subThreads.push(c);
      else roots.push(c);
    });
    return { roots: roots, subThreads: subThreads };
  }

  function createdTime(c) {
    var t = Date.parse(c && c.created_at);
    return isNaN(t) ? 0 : t;
  }

  // parent message id → its sub-threads, oldest first.
  function indexByParent(subThreads) {
    var index = new Map();
    (subThreads || []).forEach(function (s) {
      var bucket = index.get(s.parent_id);
      if (!bucket) {
        bucket = [];
        index.set(s.parent_id, bucket);
      }
      bucket.push(s);
    });
    index.forEach(function (bucket) {
      bucket.sort(function (a, b) { return createdTime(a) - createdTime(b); });
    });
    return index;
  }

  // The root thread a comment or reply belongs to, walking parent_id up
  // through sub-threads and the replies they comment on. `comments` is the
  // whole list (roots and sub-threads). Null when the chain breaks.
  function rootIdOf(id, comments) {
    var byId = new Map();
    var replyOwner = new Map();
    (comments || []).forEach(function (c) {
      byId.set(c.id, c);
      (c.replies || []).forEach(function (r) { replyOwner.set(r.id, c.id); });
    });
    var cur = id;
    // Every step moves to another message; more steps than messages is a cycle.
    for (var steps = 0; steps <= byId.size + replyOwner.size; steps++) {
      var c = byId.get(cur);
      if (!c) {
        if (!replyOwner.has(cur)) return null;
        cur = replyOwner.get(cur);
        continue;
      }
      if (!c.parent_id) return c.id;
      cur = c.parent_id;
    }
    return null;
  }

  // Every sub-thread under a message, at any depth. `message` is a comment
  // ({ id, replies }: its body and each of its replies carry sub-threads) or
  // a reply ({ id }).
  function descendants(message, index) {
    var out = [];
    var seen = new Set();
    (function walk(msg) {
      var ids = [msg.id].concat((msg.replies || []).map(function (r) { return r.id; }));
      ids.forEach(function (id) {
        (index.get(id) || []).forEach(function (sub) {
          if (seen.has(sub.id)) return;
          seen.add(sub.id);
          out.push(sub);
          walk(sub);
        });
      });
    })(message);
    return out;
  }

  // Whether an unresolved sub-thread sits anywhere under `message`.
  function hasOpenDescendant(message, index) {
    return descendants(message, index).some(function (s) { return !s.resolved; });
  }

  // Ids of every message of a list: its comments (root threads and
  // sub-threads) and their replies.
  function messageIds(comments) {
    var ids = new Set();
    (comments || []).forEach(function (c) {
      ids.add(c.id);
      (c.replies || []).forEach(function (r) { ids.add(r.id); });
    });
    return ids;
  }

  // Sub-threads whose parent message is not in the list (no comment, sub-
  // thread or reply with that id): they have nowhere to nest.
  function orphanSubThreads(roots, subThreads) {
    var ids = messageIds((roots || []).concat(subThreads || []));
    return (subThreads || []).filter(function (s) { return !ids.has(s.parent_id); });
  }

  var api = {
    MESSAGE_BODY_SELECTOR: MESSAGE_BODY_SELECTOR,
    normalizeText: normalizeText,
    normalizeWithMap: normalizeWithMap,
    quoteFromParts: quoteFromParts,
    locateQuote: locateQuote,
    messageBodyOf: messageBodyOf,
    resolveMessageSelection: resolveMessageSelection,
    quoteRange: quoteRange,
    partitionComments: partitionComments,
    indexByParent: indexByParent,
    rootIdOf: rootIdOf,
    descendants: descendants,
    hasOpenDescendant: hasOpenDescendant,
    messageIds: messageIds,
    orphanSubThreads: orphanSubThreads,
  };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.subthreads = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
