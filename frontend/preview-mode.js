(function () {
  'use strict';

  var state = {
    session: null,
    filePath: '',
    sourceLines: [],
    comments: [],
  };

  function boot() {
    if (window.crit && window.crit.shared) {
      window.crit.shared.applyThemeFromCookie();
    }
    pollSession();
  }

  function pollSession() {
    fetch('/api/session')
      .then(function (r) {
        if (r.status === 503) {
          setTimeout(pollSession, 500);
          return null;
        }
        if (!r.ok) throw new Error('session fetch failed: ' + r.status);
        return r.json();
      })
      .then(function (data) {
        if (!data) return;
        state.session = data;
        if (data.files && data.files.length > 0) {
          state.filePath = data.files[0].path;
        }
        initUI();
      })
      .catch(function (err) {
        console.error('[crit preview] session error:', err);
        setTimeout(pollSession, 1000);
      });
  }

  function initUI() {
    var container = document.createElement('div');
    container.className = 'crit-preview-container';

    var iframePane = document.createElement('div');
    iframePane.className = 'crit-preview-iframe-pane';
    var iframe = document.createElement('iframe');
    iframe.src = '/preview-content/';
    iframe.setAttribute('sandbox', 'allow-scripts allow-same-origin allow-forms allow-popups');
    iframePane.appendChild(iframe);

    var sourcePane = document.createElement('div');
    sourcePane.className = 'crit-preview-source-pane';

    container.appendChild(iframePane);
    container.appendChild(sourcePane);

    var body = document.body;
    body.appendChild(container);

    updateHeaderTitle();
    loadSource(sourcePane);
    loadComments();
    setupSSE();
  }

  function updateHeaderTitle() {
    var titleEl = document.querySelector('.header-title');
    if (titleEl) {
      titleEl.textContent = 'Preview: ' + (state.filePath || 'loading...');
    }
  }

  function loadSource(pane) {
    if (!state.filePath) return;
    fetch('/api/file?path=' + encodeURIComponent(state.filePath))
      .then(function (r) {
        if (!r.ok) throw new Error('file fetch failed');
        return r.json();
      })
      .then(function (data) {
        state.sourceLines = (data.content || '').split('\n');
        renderSource(pane);
      })
      .catch(function (err) {
        console.error('[crit preview] source load error:', err);
        pane.textContent = 'Failed to load source.';
      });
  }

  function renderSource(pane) {
    var wrap = document.createElement('div');
    wrap.className = 'source-content';

    var commentsByLine = {};
    state.comments.forEach(function (c) {
      var line = c.end_line || c.start_line;
      if (!commentsByLine[line]) commentsByLine[line] = [];
      commentsByLine[line].push(c);
    });

    state.sourceLines.forEach(function (text, i) {
      var lineNum = i + 1;
      var row = document.createElement('div');
      row.className = 'source-line';
      if (commentsByLine[lineNum]) {
        row.classList.add('source-line-commented');
      }

      var gutter = document.createElement('span');
      gutter.className = 'source-gutter';
      gutter.textContent = lineNum;
      gutter.addEventListener('click', function () {
        openCommentForm(lineNum, pane, row);
      });

      var code = document.createElement('span');
      code.className = 'source-text';
      code.textContent = text;

      row.appendChild(gutter);
      row.appendChild(code);
      wrap.appendChild(row);

      if (commentsByLine[lineNum]) {
        commentsByLine[lineNum].forEach(function (c) {
          var card = renderCommentCard(c);
          wrap.appendChild(card);
        });
      }
    });

    pane.innerHTML = '';
    pane.appendChild(wrap);
  }

  function renderCommentCard(comment) {
    var div = document.createElement('div');
    div.className = 'crit-preview-comment-inline';
    if (window.crit && window.crit.commentCard && window.crit.commentCard.buildCommentCard) {
      var card = window.crit.commentCard.buildCommentCard(comment, {
        onReply: function (body) { postReply(comment, body); },
        onResolve: function () { resolveComment(comment); },
      });
      div.appendChild(card);
    } else {
      var p = document.createElement('p');
      p.textContent = (comment.author || 'Anonymous') + ': ' + comment.body;
      div.appendChild(p);
    }
    return div;
  }

  function openCommentForm(lineNum, pane, afterEl) {
    var existing = pane.querySelector('.crit-preview-inline-form');
    if (existing) existing.remove();

    var formWrap = document.createElement('div');
    formWrap.className = 'crit-preview-comment-inline crit-preview-inline-form';

    if (window.crit && window.crit.commentForm && window.crit.commentForm.createCommentForm) {
      var form = window.crit.commentForm.createCommentForm({
        onSubmit: function (body) {
          postComment(lineNum, body);
          formWrap.remove();
        },
        onCancel: function () { formWrap.remove(); },
        placeholder: 'Comment on line ' + lineNum + '...',
      });
      formWrap.appendChild(form);
    } else {
      var ta = document.createElement('textarea');
      ta.placeholder = 'Comment on line ' + lineNum + '...';
      ta.style.cssText = 'width:100%;min-height:60px;margin-bottom:8px;';
      var btn = document.createElement('button');
      btn.textContent = 'Submit';
      btn.addEventListener('click', function () {
        if (ta.value.trim()) {
          postComment(lineNum, ta.value.trim());
          formWrap.remove();
        }
      });
      var cancel = document.createElement('button');
      cancel.textContent = 'Cancel';
      cancel.style.marginLeft = '8px';
      cancel.addEventListener('click', function () { formWrap.remove(); });
      formWrap.appendChild(ta);
      formWrap.appendChild(btn);
      formWrap.appendChild(cancel);
    }

    afterEl.parentNode.insertBefore(formWrap, afterEl.nextSibling);
  }

  function postComment(lineNum, body) {
    fetch('/api/file/comments?path=' + encodeURIComponent(state.filePath), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        start_line: lineNum,
        end_line: lineNum,
        body: body,
      }),
    })
      .then(function (r) {
        if (!r.ok) throw new Error('post comment failed');
        return r.json();
      })
      .then(function () { loadComments(); })
      .catch(function (err) { console.error('[crit preview] comment error:', err); });
  }

  function postReply(comment, body) {
    fetch('/api/comment/' + comment.id + '/replies?path=' + encodeURIComponent(state.filePath), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ body: body }),
    })
      .then(function (r) {
        if (!r.ok) throw new Error('reply failed');
        loadComments();
      })
      .catch(function (err) { console.error('[crit preview] reply error:', err); });
  }

  function resolveComment(comment) {
    fetch('/api/comment/' + comment.id + '/resolve?path=' + encodeURIComponent(state.filePath), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ resolved: true }),
    })
      .then(function (r) {
        if (!r.ok) throw new Error('resolve failed');
        loadComments();
      })
      .catch(function (err) { console.error('[crit preview] resolve error:', err); });
  }

  function loadComments() {
    if (!state.filePath) return;
    fetch('/api/file/comments?path=' + encodeURIComponent(state.filePath))
      .then(function (r) {
        if (!r.ok) throw new Error('comments fetch failed');
        return r.json();
      })
      .then(function (data) {
        state.comments = data.comments || data || [];
        var pane = document.querySelector('.crit-preview-source-pane');
        if (pane) renderSource(pane);
      })
      .catch(function (err) { console.error('[crit preview] comments error:', err); });
  }

  function setupSSE() {
    if (window.crit && window.crit.sse && window.crit.sse.createSSE) {
      window.crit.sse.createSSE('/api/events', {
        'comments-changed': function () { loadComments(); },
      });
    } else {
      var es = new EventSource('/api/events');
      es.addEventListener('comments-changed', function () { loadComments(); });
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot);
  } else {
    boot();
  }
})();
