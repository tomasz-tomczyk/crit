'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.design = root.crit.design || {};
    root.crit.design.driftTray = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, ch => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[ch]));
  }

  function renderRow(r) {
    const isRecoverable = r.status === 'drifted-recoverable';
    const badgeClass = isRecoverable
      ? 'crit-design-drifted-badge--recoverable'
      : 'crit-design-drifted-badge--lost';
    const badgeText = isRecoverable ? 'Drifted (recoverable)' : 'Drifted';
    const reanchor = isRecoverable
      ? `<button type="button" class="crit-design-reanchor-btn" data-pin-id="${escapeHTML(r.id)}">Re-anchor here?</button>`
      : '';
    const truncated = (r.body || '').slice(0, 120);
    return `<li class="crit-design-drifted-row" data-pin-id="${escapeHTML(r.id)}">` +
      `<span class="crit-design-drifted-route">${escapeHTML(r.pathname || '')}</span>` +
      `<span class="crit-design-drifted-badge ${badgeClass}">${escapeHTML(badgeText)}</span>` +
      `<span class="crit-design-drifted-body">${escapeHTML(truncated)}</span>` +
      reanchor +
    `</li>`;
  }

  function renderDriftTrayHTML(rows, _currentRound) {
    if (!rows || !rows.length) return '';
    // Single flat list. The earlier per-round partition with prominent
    // "Drifted on round N" / "Drifted earlier" headings was redundant: the
    // main panel already lists every pin with its own Drifted badge, and
    // each row carries its pathname inline. The tray now exists solely to
    // surface the Re-anchor affordance for recoverable pins. _currentRound
    // is kept as a parameter for call-site stability.
    const items = rows.map(renderRow).join('');
    return `<ul class="crit-design-drifted-tray" aria-label="Drifted pins">${items}</ul>`;
  }

  return { renderDriftTrayHTML, escapeHTML };
});
