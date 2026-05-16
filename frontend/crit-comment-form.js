(function () {
  'use strict';

  /**
   * createForm(opts) → { el, focus, getBody, setBody, destroy }
   *
   * Shared comment form creation used by both code-review and design-mode.
   * The caller decides WHERE to insert and WHAT to do on submit/cancel.
   *
   * opts:
   *   formKey          — unique form ID (for draft, data-attribute)
   *   headerText       — header label (e.g. 'Comment on lines 5-10')
   *   initialBody      — pre-fill text (from draft or editing)
   *   placeholder      — textarea placeholder
   *   submitLabel      — submit button text ('Comment', 'Save', 'Reply')
   *   onSubmit(body)   — called with trimmed body on submit
   *   onCancel()       — called on cancel (after confirm if dirty)
   *   onInput(body)    — called on every input (for draft saving)
   *   confirmDiscard   — function returning bool; gates Escape cancel
   *   autoFocus        — focus textarea on creation (default false)
   *   compact          — smaller form for replies (default false)
   *   showHeader       — show the header element (default true)
   *   showTemplates    — show template bar (default true)
   *   extraButtons     — array of { className, innerHTML, title, onClick }
   */
  function createForm(opts) {
    opts = opts || {};

    var wrapper = document.createElement('div');
    wrapper.className = 'comment-form-wrapper' + (opts.compact ? ' comment-form-wrapper--compact' : '');

    var form = document.createElement('div');
    form.className = 'comment-form';
    if (opts.formKey) form.dataset.formKey = opts.formKey;

    if (opts.showHeader !== false && opts.headerText) {
      var header = document.createElement('div');
      header.className = 'comment-form-header';
      header.textContent = opts.headerText;
      form.appendChild(header);
    }

    var textarea = document.createElement('textarea');
    textarea.placeholder = opts.placeholder || 'Leave a comment... (Ctrl+Enter to submit, Escape to cancel)';
    if (opts.formKey) textarea.dataset.formKey = opts.formKey;
    if (opts.initialBody) textarea.value = opts.initialBody;

    // Wire image uploads if available
    if (window.crit && window.crit.shared && window.crit.shared.attachImageUploads) {
      window.crit.shared.attachImageUploads(textarea);
    }

    // Input handler for draft saving
    if (typeof opts.onInput === 'function') {
      textarea.addEventListener('input', function () {
        opts.onInput(textarea.value);
      });
    }

    // Keyboard shortcuts
    textarea.addEventListener('keydown', function (e) {
      if (e.isComposing) return;
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        doSubmit();
        return;
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        doCancelFromEsc();
      }
    });

    form.appendChild(textarea);

    // Actions bar
    var actions = document.createElement('div');
    actions.className = 'comment-form-actions';

    var cancelBtn = document.createElement('button');
    cancelBtn.type = 'button';
    cancelBtn.className = 'btn btn-sm';
    cancelBtn.textContent = 'Cancel';
    cancelBtn.addEventListener('click', doCancel);

    var submitBtn = document.createElement('button');
    submitBtn.type = 'button';
    submitBtn.className = 'btn btn-sm btn-primary';
    submitBtn.textContent = opts.submitLabel || 'Comment';
    submitBtn.addEventListener('click', doSubmit);

    actions.appendChild(cancelBtn);
    actions.appendChild(submitBtn);

    // Extra buttons (e.g. "Send now" for agent)
    if (opts.extraButtons && opts.extraButtons.length) {
      for (var i = 0; i < opts.extraButtons.length; i++) {
        var spec = opts.extraButtons[i];
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = spec.className || 'btn btn-sm';
        if (spec.innerHTML) btn.innerHTML = spec.innerHTML;
        if (spec.title) btn.title = spec.title;
        if (typeof spec.onClick === 'function') {
          btn.addEventListener('click', spec.onClick);
        }
        actions.appendChild(btn);
      }
    }

    form.appendChild(actions);

    // Template bar integration
    if (opts.showTemplates !== false && window.crit && window.crit.commentTemplates &&
        typeof window.crit.commentTemplates.buildTemplateBar === 'function') {
      try {
        var bar = window.crit.commentTemplates.buildTemplateBar({
          onInsert: function (text) {
            textarea.value = text;
            textarea.focus();
            textarea.dispatchEvent(new Event('input', { bubbles: true }));
          },
        });
        if (bar) form.insertBefore(bar, actions);
      } catch (_) {}
    }

    wrapper.appendChild(form);

    if (opts.autoFocus) {
      requestAnimationFrame(function () { textarea.focus(); });
    }

    function doSubmit() {
      var body = textarea.value.trim();
      if (!body) { textarea.focus(); return; }
      if (typeof opts.onSubmit === 'function') opts.onSubmit(body);
    }

    function doCancel() {
      if (typeof opts.onCancel === 'function') opts.onCancel();
    }

    function doCancelFromEsc() {
      if (typeof opts.confirmDiscard === 'function') {
        if (!opts.confirmDiscard()) return;
      } else {
        var dirty = textarea.value.trim().length > 0 && textarea.value !== (opts.initialBody || '');
        if (dirty && !window.confirm('Discard comment?')) return;
      }
      doCancel();
    }

    return {
      el: wrapper,
      focus: function () { textarea.focus(); },
      getBody: function () { return textarea.value; },
      setBody: function (v) { textarea.value = v; },
      getTextarea: function () { return textarea; },
      destroy: function () { wrapper.remove(); },
    };
  }

  var api = { createForm: createForm };

  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.commentForm = api;
  }

  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
