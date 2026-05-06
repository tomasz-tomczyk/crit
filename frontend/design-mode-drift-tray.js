'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else { root.crit = root.crit || {}; root.crit.designModeDriftTray = api; }
})(typeof window !== 'undefined' ? window : globalThis, function () {

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, ch => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    }[ch]));
  }

  function renderDriftTrayHTML(rows) {
    if (!rows || !rows.length) return '';
    const items = rows.map(r => {
      const isRecoverable = r.status === 'drifted-recoverable';
      const badgeClass = isRecoverable
        ? 'crit-design-drifted-badge--recoverable'
        : 'crit-design-drifted-badge--lost';
      const badgeText = isRecoverable ? 'Drifted (recoverable)' : 'Drifted';
      const reanchor = isRecoverable
        ? `<button type="button" class="crit-design-reanchor-btn" data-pin-id="${escapeHTML(r.id)}">Re-anchor here?</button>`
        : '';
      // Truncate raw body BEFORE escaping so multi-char entities don't get cut.
      const truncated = (r.body || '').slice(0, 120);
      return `<li class="crit-design-drifted-row" data-pin-id="${escapeHTML(r.id)}">` +
        `<span class="crit-design-drifted-route">${escapeHTML(r.pathname || '')}</span>` +
        `<span class="crit-design-drifted-badge ${badgeClass}">${escapeHTML(badgeText)}</span>` +
        `<span class="crit-design-drifted-body">${escapeHTML(truncated)}</span>` +
        reanchor +
      `</li>`;
    }).join('');
    return `<ul class="crit-design-drifted-tray" aria-label="Drifted pins">${items}</ul>`;
  }

  return { renderDriftTrayHTML, escapeHTML };
});
