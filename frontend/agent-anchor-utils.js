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
  return { implicitRole };
});
