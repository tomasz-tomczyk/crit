// crit-read-aloud.js — read the reviewed markdown documents aloud with the
// browser's speech synthesis (Web Speech API). No audio leaves the machine and
// nothing is fetched: the voices are the OS ones the browser exposes.
//
// Two parts:
//   segmentsFromBlocks — pure: Crit line blocks → spoken segments.
//   createReader       — speaks segments one utterance per block, so the page
//                        can highlight the block being read, with pause,
//                        seek (by block), speed and volume.
//
// Exports onto window.crit.readAloud. No dependencies on other crit modules.

(function () {
  'use strict';

  // Block HTML is already sanitized, but parse it in an inert document anyway
  // so images in it never load while we only want the text.
  function defaultHtmlToText(html) {
    if (typeof DOMParser !== 'undefined') {
      return new DOMParser().parseFromString(html, 'text/html').body.textContent || '';
    }
    return html.replace(/<[^>]*>/g, '');
  }

  // Closing tags that separate words in the rendered text (table cells, list
  // items, headings) — textContent would otherwise run them together.
  var BREAKING_TAGS = /<\/(td|th|li|p|h[1-6]|div|tr|blockquote)>|<br\s*\/?>/gi;

  function hasClass(block, name) {
    return (' ' + (block.cssClass || '') + ' ').indexOf(' ' + name + ' ') !== -1;
  }

  // Turn one file's line blocks into the segments to speak. Code is announced
  // once ("Code block.") rather than read line by line, and diagrams likewise.
  function segmentsFromBlocks(filePath, blocks, htmlToText) {
    var toText = htmlToText || defaultHtmlToText;
    var segments = [];
    for (var i = 0; i < (blocks || []).length; i++) {
      var block = blocks[i];
      if (block.isEmpty || hasClass(block, 'table-separator')) continue;
      var text;
      if (hasClass(block, 'render-block')) {
        text = 'Diagram.';
      } else if (hasClass(block, 'code-line')) {
        if (!hasClass(block, 'code-first')) continue;
        text = 'Code block.';
      } else {
        text = toText(String(block.html || '').replace(BREAKING_TAGS, '$& '))
          .replace(/\s+/g, ' ').trim();
      }
      if (!text) continue;
      segments.push({ filePath: filePath, startLine: block.startLine, endLine: block.endLine, text: text });
    }
    return segments;
  }

  // Index of the segment to start from for a block at (filePath, line): the
  // first segment of that file ending at or after the line, else 0.
  function startIndexFor(segments, filePath, line) {
    for (var i = 0; i < segments.length; i++) {
      if (segments[i].filePath === filePath && segments[i].endLine >= line) return i;
    }
    return 0;
  }

  // Index of the segment covering (filePath, line): the last segment of that
  // file starting at or before the line, so a click inside a code block lands
  // on its "Code block." announcement. Falls back to startIndexFor.
  function indexAtLine(segments, filePath, line) {
    var found = -1;
    for (var i = 0; i < segments.length; i++) {
      if (segments[i].filePath !== filePath) continue;
      if (segments[i].startLine > line) break;
      found = i;
    }
    return found !== -1 ? found : startIndexFor(segments, filePath, line);
  }

  // Speeds offered by the player, slowest first.
  var RATES = [0.75, 1, 1.25, 1.5, 1.75, 2];

  function clamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  // opts: { synth, Utterance, rate, volume, onSegment(segment, index),
  //         onStateChange(state) } — state is 'stopped' | 'playing' | 'paused'.
  //
  // Pause is cancel-and-remember rather than synth.pause(): Chrome's pause is
  // unreliable (it resumes nothing for some voices), and a block is short, so
  // resuming restarts the current block. Rate and volume changes and seeks
  // restart the current block the same way, so they apply at once.
  function createReader(opts) {
    var synth = opts.synth;
    var Utterance = opts.Utterance;
    var segments = [];
    var index = 0;
    var state = 'stopped';
    var rate = clamp(Number(opts.rate) || 1, 0.5, 3);
    var volume = clamp(opts.volume == null ? 1 : Number(opts.volume), 0, 1);
    // Bumped whenever the current utterance is abandoned, so the end or error
    // a browser fires for a cancelled utterance is ignored.
    var generation = 0;

    function setState(value) {
      if (state === value) return;
      state = value;
      if (opts.onStateChange) opts.onStateChange(state);
    }

    function speakCurrent(gen) {
      if (gen !== generation) return;
      if (index >= segments.length) {
        index = 0;
        setState('stopped');
        return;
      }
      var segment = segments[index];
      var utterance = new Utterance(segment.text);
      utterance.rate = rate;
      utterance.volume = volume;
      utterance.onend = function () {
        if (gen !== generation) return;
        index++;
        speakCurrent(gen);
      };
      utterance.onerror = function () {
        if (gen !== generation) return;
        generation++;
        setState('stopped');
      };
      if (opts.onSegment) opts.onSegment(segment, index);
      synth.speak(utterance);
    }

    // Abandon the current utterance and, when playing, speak from `index`.
    function restart() {
      generation++;
      synth.cancel();
      if (state === 'playing') speakCurrent(generation);
    }

    function start(list, startIndex) {
      segments = list || [];
      index = clamp(startIndex || 0, 0, Math.max(0, segments.length - 1));
      setState(segments.length ? 'playing' : 'stopped');
      restart();
    }

    function stop() {
      setState('stopped');
      restart();
    }

    function pause() {
      if (state !== 'playing') return;
      setState('paused');
      restart();
    }

    function resume() {
      if (state !== 'paused') return;
      setState('playing');
      restart();
    }

    // Jump to a block. While paused, the block is shown but not spoken.
    function seek(to) {
      if (state === 'stopped' || !segments.length) return;
      index = clamp(to, 0, segments.length - 1);
      if (state === 'paused' && opts.onSegment) opts.onSegment(segments[index], index);
      restart();
    }

    function setRate(value) {
      rate = clamp(Number(value) || 1, 0.5, 3);
      restart();
    }

    function setVolume(value) {
      volume = clamp(Number(value), 0, 1);
      restart();
    }

    return {
      start: start,
      stop: stop,
      pause: pause,
      resume: resume,
      seek: seek,
      setRate: setRate,
      setVolume: setVolume,
      state: function () { return state; },
      index: function () { return index; },
      length: function () { return segments.length; },
    };
  }

  function isSupported(root) {
    return !!(root && root.speechSynthesis && root.SpeechSynthesisUtterance);
  }

  var api = {
    segmentsFromBlocks: segmentsFromBlocks,
    startIndexFor: startIndexFor,
    indexAtLine: indexAtLine,
    createReader: createReader,
    RATES: RATES,
    isSupported: isSupported,
  };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.readAloud = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
