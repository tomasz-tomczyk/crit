'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.live = root.crit.live || {};
    root.crit.live.reanchorClick = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  function handleTrayClick(ev, post) {
    const t = ev && ev.target;
    if (!t || typeof t.matches !== 'function') return;
    if (!t.matches('.crit-live-reanchor-btn')) return;
    const pinId = t.getAttribute('data-pin-id');
    if (!pinId) return;
    post({ type: 'enter-reanchor-mode', pin_id: pinId });
  }

  // Controller: arms re-anchor with disable + 30s timeout.
  function armReanchor(ctx, pinId, btnEl) {
    if (ctx.state.reanchorPending) return;
    ctx.state.reanchorPending = pinId;
    if (btnEl) btnEl.disabled = true;
    ctx.state.reanchorBtn = btnEl;
    const setT = ctx.setTimeout || setTimeout;
    ctx.state.reanchorTimeoutId = setT(() => disarmReanchor(ctx, 'timeout'), 30000);
    if (typeof ctx.post === 'function') ctx.post({ type: 'enter-reanchor-mode', pin_id: pinId });
  }
  function disarmReanchor(ctx, reason) {
    if (ctx.state.reanchorBtn) ctx.state.reanchorBtn.disabled = false;
    if (ctx.state.reanchorTimeoutId) {
      const clearT = ctx.clearTimeout || clearTimeout;
      clearT(ctx.state.reanchorTimeoutId);
    }
    ctx.state.reanchorPending = null;
    ctx.state.reanchorBtn = null;
    ctx.state.reanchorTimeoutId = null;
    if (reason === 'timeout' && typeof ctx.toast === 'function') {
      ctx.toast('Re-anchor cancelled — click the button again to retry.');
    }
  }

  return { handleTrayClick, armReanchor, disarmReanchor };
});
