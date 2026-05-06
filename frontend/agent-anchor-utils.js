'use strict';
(function (root, factory) {
  const api = factory();
  if (typeof module === 'object' && module.exports) module.exports = api;
  else {
    root.crit = root.crit || {};
    root.crit.agent = root.crit.agent || {};
    root.crit.agent.anchorUtils = api;
  }
})(typeof window !== 'undefined' ? window : globalThis, function () {
  const IMPLICIT_ROLES = {
    A: 'link', AREA: 'link',
    BUTTON: 'button',
    NAV: 'navigation',
    MAIN: 'main',
    HEADER: 'banner',
    FOOTER: 'contentinfo',
    ASIDE: 'complementary',
    SECTION: 'region',
    ARTICLE: 'article',
    H1: 'heading', H2: 'heading', H3: 'heading',
    H4: 'heading', H5: 'heading', H6: 'heading',
    UL: 'list', OL: 'list',
    LI: 'listitem',
    IMG: 'img',
    INPUT: 'textbox',
    SELECT: 'combobox',
    TEXTAREA: 'textbox',
    FORM: 'form',
    TABLE: 'table',
    THEAD: 'rowgroup', TBODY: 'rowgroup', TFOOT: 'rowgroup',
    TR: 'row',
    TH: 'columnheader',
    TD: 'cell',
    DIALOG: 'dialog',
  };
  function implicitRole(tagName) {
    if (typeof tagName !== 'string') return '';
    return IMPLICIT_ROLES[tagName.toUpperCase()] || '';
  }

  // Resolution risk: nearest-id semantics may pick a parent whose id is reused or
  // later renamed in the user app, breaking the css_selector across page redeploys.
  // Phase D drift detection re-resolves selections using `tag_chain` +
  // `accessible_name` + `landmark` as fallback fields when the selector misses.
  function findAnchorRoot(el) {
    let cur = el;
    while (cur) {
      if (cur.id) return cur;
      if (cur.tagName === 'BODY') return cur;
      cur = cur.parentNode;
    }
    return el;
  }

  return { implicitRole, findAnchorRoot };
});
