// crit-share.js — shared Share flow for code-review (app.js) and live/preview
// (live-mode.js) modes.
//
// Exports a factory onto window.crit.share. create(options) builds a controller
// that owns ALL share state internally (hostedURL, deleteToken, tokens, org,
// in-flight guard, the open modal element, the org cache) and wires the
// #shareBtn click handler onto options.shareBtnEl.
//
// Transport, not protocol: when proxy_auth is off, Share / Pull / Re-share /
// Unpublish go through the local Go server (/api/share, /api/share/pull,
// /api/share/reshare, /api/share-url), which attaches the bearer token. When
// proxy_auth is on, the popup relay hits the same crit-web API endpoints with
// the same payload shapes; the only branch is which payload-builder endpoint
// is called for preview vs files (see performShare). The browser never calls
// crit-web cross-origin in either mode.
//
// Dependencies (via window.crit.*):
//   - none at module scope; all collaborators are injected through options.
//
// options (all flat top-level keys, not nested):
//   config fields: shareURL, hostedURL, deleteToken, hostedToken,
//             needsShareConsent, authUserName, proxyAuth, reviewType,
//             sharedOrg, sharedVisibility
//   shareBtnEl: the #shareBtn element (click handler attached internally)
//   canShare:  bool — reveal() shows the button iff (shareURL && canShare).
//              code-review passes session.mode !== 'git'; preview passes true.
//   adapters:
//     onCommentsRefreshed():        re-render the comments UI after a merge
//     toast: { show(id,type,html,opts)->el, dismiss(id) }
//     escapeHtml(s)
//     getSetting(key, fallback), setSetting(key, value)
//     icons: { clipboard, check }  // SVG strings
//
// Controller: { reveal(), setButtonState(state), openModal(), closeModal() }
(function () {
  'use strict';

  function create(options) {
    options = options || {};

    // ---- injected collaborators ----
    var toast = options.toast || { show: function () { return document.createElement('div'); }, dismiss: function () {} };
    var escapeHtml = options.escapeHtml || function (s) { return s == null ? '' : String(s); };
    var getSetting = options.getSetting || function (_k, fb) { return fb; };
    var setSetting = options.setSetting || function () {};
    var onCommentsRefreshed = options.onCommentsRefreshed || function () { return Promise.resolve(); };
    var icons = options.icons || {};
    var ICON_CLIPBOARD = icons.clipboard || '';
    var ICON_CHECK_SMALL = icons.check || '';

    function showToast(id, type, content, opts) { return toast.show(id, type, content, opts); }
    function dismissToast(id) { return toast.dismiss(id); }

    // ---- config (snapshot from options; mutated as share state evolves) ----
    var shareTargets = Array.isArray(options.shareTargets) ? options.shareTargets.slice() : [];
    if (shareTargets.length === 0 && options.shareURL) {
      shareTargets = [{ name: options.shareURL, url: options.shareURL, default: true, proxy_auth: !!options.proxyAuth, auth_user_name: options.authUserName || '', needs_share_consent: !!options.needsShareConsent }];
    }
    var shareBaseURL = options.shareBaseURL || '';
    var selectedTarget = shareBaseURL
      ? (shareTargets.find(function(t) { return t.url === shareBaseURL; }) || null)
      : (shareTargets.find(function(t) { return t.default; }) || (shareTargets.length === 1 ? shareTargets[0] : null));
    var shareURL = selectedTarget ? selectedTarget.url : '';
    var hostedURL = options.hostedURL || '';
    var deleteToken = options.deleteToken || '';
    var hostedToken = options.hostedToken || '';
    var needsShareConsent = selectedTarget ? !!selectedTarget.needs_share_consent : !!options.needsShareConsent;
    var authUserName = selectedTarget ? (selectedTarget.auth_user_name || '') : (options.authUserName || '');
    var proxyAuth = selectedTarget ? !!selectedTarget.proxy_auth : !!options.proxyAuth;
    var reviewType = options.reviewType || '';
    var sharedOrg = options.sharedOrg || null;
    var sharedVisibility = options.sharedVisibility || '';

    // ---- internal share state ----
    var cachedOrgs = new Map();
    var fetchOrgsPromise = new Map();
    var cachedSharePolicy = new Map();
    var fetchSharePolicyPromise = new Map();
    var shareInFlight = false;
    var shareModalEl = null;

    var shareBtnEl = options.shareBtnEl || null;

    function selectTarget(target) {
      selectedTarget = target;
      shareURL = target ? target.url : '';
      proxyAuth = target ? !!target.proxy_auth : false;
      authUserName = target ? (target.auth_user_name || '') : '';
      needsShareConsent = target ? !!target.needs_share_consent : false;
    }

    // openShareReceiver(shareURL) opens the crit-web /share-receiver page in a
    // popup and brokers same-origin API calls through it. Fully mode-agnostic.
    //
    //   1. window.open MUST be called synchronously inside the user-gesture.
    //      Any `await` before this call will cause Safari (and often Chrome/
    //      Firefox) to popup-block the window.open.
    //   2. The 'ready' postMessage listener is attached BEFORE window.open so
    //      the receiver's 'ready' message can't arrive before we listen.
    //   3. After the handshake, all communication is via the MessagePort. The
    //      port has no origin — it's a private channel. event.origin is only
    //      validated on the handshake postMessage.
    //   4. Single persistent port.onmessage with a requestId -> resolver Map.
    //      No listener-per-op accumulation.
    //   5. A close-watchdog (setInterval) rejects pending ops if the popup
    //      closes (user dismisses, navigates away, crashes).
    function openShareReceiver(baseURL) {
      if (!baseURL) throw new Error('share_url not configured');

      const nonce = 'n_' + Math.random().toString(36).slice(2) + Date.now().toString(36);
      const targetOrigin = new URL(baseURL).origin;
      const popupURL = baseURL.replace(/\/$/, '') + '/share-receiver#nonce=' + encodeURIComponent(nonce);

      let popup = null;
      let port = null;
      let readyResolve, readyReject;
      const readyPromise = new Promise(function(res, rej) { readyResolve = res; readyReject = rej; });

      const pending = new Map(); // requestId -> { resolve, reject, timer }
      function rejectAllPending(err) {
        for (const entry of pending.values()) {
          clearTimeout(entry.timer);
          entry.reject(err);
        }
        pending.clear();
      }

      function onPortMessage(event) {
        const msg = event.data || {};
        const entry = pending.get(msg.requestId);
        if (!entry) return;
        pending.delete(msg.requestId);
        clearTimeout(entry.timer);
        if (msg.ok) entry.resolve(msg.data);
        else entry.reject(new Error(msg.error || 'unknown error'));
      }

      function onReady(event) {
        // Validate handshake: source must be our popup, origin must match,
        // payload must be `{type: 'ready', nonce}`.
        if (popup && event.source !== popup) return;
        if (event.origin !== targetOrigin) return;
        if (!event.data || event.data.type !== 'ready' || event.data.nonce !== nonce) return;
        window.removeEventListener('message', onReady);
        const channel = new MessageChannel();
        port = channel.port1;
        port.onmessage = onPortMessage;
        port.start();
        try {
          popup.postMessage({ type: 'init', nonce: nonce }, targetOrigin, [channel.port2]);
        } catch (err) {
          readyReject(err);
          return;
        }
        readyResolve();
      }

      // Attach listener BEFORE opening the popup so the receiver's 'ready'
      // postMessage cannot arrive before we are listening.
      window.addEventListener('message', onReady);

      popup = window.open(popupURL, 'crit_share_receiver', 'width=520,height=640,resizable=yes,scrollbars=yes');
      if (!popup) {
        window.removeEventListener('message', onReady);
        throw new Error('popup blocked — allow popups for this page');
      }

      // Watchdog: if the popup closes before init or mid-op, reject everything.
      const closeWatch = setInterval(function() {
        if (popup.closed) {
          clearInterval(closeWatch);
          window.removeEventListener('message', onReady);
          readyReject(new Error('popup closed before authenticating'));
          rejectAllPending(new Error('popup closed'));
        }
      }, 500);

      // Init handshake timeout: if the receiver never posts 'ready' (COOP, popup
      // never navigated, ad blocker, etc.) reject after 60s.
      const initTimer = setTimeout(function() {
        window.removeEventListener('message', onReady);
        readyReject(new Error('share-receiver did not respond — possibly blocked by COOP'));
      }, 60000);
      readyPromise.finally(function() { clearTimeout(initTimer); });

      return {
        ready: readyPromise,
        async run(op, data, timeoutMs) {
          await readyPromise;
          const requestId = nonce + '_' + op + '_' + Math.random().toString(36).slice(2);
          return new Promise(function(resolve, reject) {
            const timer = setTimeout(function() {
              pending.delete(requestId);
              reject(new Error('share-receiver did not return a result for ' + op));
            }, timeoutMs || 120000);
            pending.set(requestId, { resolve: resolve, reject: reject, timer: timer });
            port.postMessage(Object.assign({}, data, { type: op, requestId: requestId }));
          });
        },
        close() {
          clearInterval(closeWatch);
          window.removeEventListener('message', onReady);
          try { popup.close(); } catch { /* popup may already be closed */ }
          try { if (port) port.close(); } catch { /* port may already be closed */ }
          rejectAllPending(new Error('session closed'));
        },
      };
    }

    // ===== Share =====
    function setShareButtonState(state) {
      const btn = document.getElementById('shareBtn');
      if (!btn) return;
      if (state === 'shared') {
        btn.textContent = 'Shared';
        btn.classList.add('btn-success');
        btn.disabled = false;
      } else if (state === 'sharing') {
        btn.textContent = 'Sharing…';
        btn.classList.remove('btn-success');
        btn.disabled = true;
      } else {
        btn.textContent = 'Share';
        btn.classList.remove('btn-success');
        btn.disabled = false;
      }
    }

    function closeShareModal() {
      if (shareModalEl) {
        shareModalEl.remove();
        shareModalEl = null;
        const trigger = document.getElementById('shareBtn');
        if (trigger) trigger.focus();
      }
    }

    async function fetchOrgs() {
	  const key = shareURL;
      if (cachedOrgs.has(key)) return cachedOrgs.get(key);
      if (fetchOrgsPromise.has(key)) return fetchOrgsPromise.get(key);
      const pending = (async function() {
        try {
          const resp = await fetch('/api/auth/orgs?target_url=' + encodeURIComponent(key));
          if (!resp.ok) { cachedOrgs.set(key, []); return []; }
          cachedOrgs.set(key, await resp.json());
        } catch {
          cachedOrgs.set(key, []);
        }
        fetchOrgsPromise.delete(key);
        return cachedOrgs.get(key);
      })();
      fetchOrgsPromise.set(key, pending);
      return pending;
    }

    const DEFAULT_SHARE_POLICY = {
      allowed_comment_policies: ['open', 'logged_in_only', 'disallowed'],
      allowed_review_visibilities: ['organization', 'unlisted', 'public'],
    };

    function normalizeSharePolicy(raw) {
      const validVisibilities = DEFAULT_SHARE_POLICY.allowed_review_visibilities;
      const rawVisibilities = raw && Array.isArray(raw.allowed_review_visibilities)
        ? raw.allowed_review_visibilities
        : validVisibilities;
      const allowedVisibilities = rawVisibilities.filter(function(v) {
        return validVisibilities.indexOf(v) !== -1;
      });

      const rawCommentPolicies = raw && Array.isArray(raw.allowed_comment_policies)
        ? raw.allowed_comment_policies
        : DEFAULT_SHARE_POLICY.allowed_comment_policies;

      return {
        allowed_comment_policies: rawCommentPolicies,
        allowed_review_visibilities: allowedVisibilities.length > 0 ? allowedVisibilities : validVisibilities,
      };
    }

    function clearSharePolicyCache() {
      cachedSharePolicy.delete(shareURL);
      fetchSharePolicyPromise.delete(shareURL);
    }

    async function fetchSharePolicy(popupSession) {
	  const key = shareURL;
      if (cachedSharePolicy.has(key)) return cachedSharePolicy.get(key);
      if (fetchSharePolicyPromise.has(key)) return fetchSharePolicyPromise.get(key);
      const pending = (async function() {
        try {
          if (popupSession) {
            cachedSharePolicy.set(key, normalizeSharePolicy(await popupSession.run('sharePolicy', {}, 30000)));
          } else {
            const resp = await fetch('/api/share-policy?target_url=' + encodeURIComponent(key));
            if (!resp.ok) {
              cachedSharePolicy.set(key, normalizeSharePolicy(null));
              return cachedSharePolicy.get(key);
            }
            cachedSharePolicy.set(key, normalizeSharePolicy(await resp.json()));
          }
        } catch {
          cachedSharePolicy.set(key, normalizeSharePolicy(null));
        }
        fetchSharePolicyPromise.delete(key);
        return cachedSharePolicy.get(key);
      })();
      fetchSharePolicyPromise.set(key, pending);
      return pending;
    }

    function sharePolicyAllowsVisibility(policy, visibility) {
      const normalized = normalizeSharePolicy(policy);
      return normalized.allowed_review_visibilities.indexOf(visibility) !== -1;
    }

    function isSharePolicyRejection(err) {
      const msg = (err && err.message ? err.message : String(err || '')).toLowerCase();
      return msg.indexOf('visibility') !== -1 ||
        msg.indexOf('comment_policy') !== -1 ||
        msg.indexOf('comment policy') !== -1 ||
        msg.indexOf('not allowed') !== -1 ||
        msg.indexOf('disallowed') !== -1;
    }

    function shareErrorMessage(err) {
      if (isSharePolicyRejection(err)) {
        return 'That sharing option is not allowed by this Crit instance. Choose another option and try again.';
      }
      return err && err.message ? err.message : String(err || 'unknown error');
    }

    async function performShare(org, visibility, orgMeta, popupSession) {
      if (shareInFlight) return;
      shareInFlight = true;

      setShareButtonState('sharing');
      dismissToast('share');
      try {
        let result;
        if (popupSession) {
          // Preview sessions crawl their HTML origin + assets server-side and
          // tag the payload review_type=preview; the relay still hits the same
          // POST /api/reviews endpoint (transport, not protocol).
          const payloadPath = reviewType === 'preview'
            ? '/api/share/preview-payload'
            : '/api/share/payload';
          const payloadResp = await fetch(payloadPath);
          if (!payloadResp.ok) {
            const errBody = await payloadResp.json().catch(function() { return {}; });
            throw new Error(errBody.error || 'failed to build share payload');
          }
          const payload = await payloadResp.json();
          if (org) payload.org = org;
          if (visibility) payload.visibility = visibility;
          if (orgMeta && orgMeta.name) payload.org_name = orgMeta.name;
          result = await popupSession.run('share', { payload: payload });

          const persistResp = await fetch('/api/share-url', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              url: result.url,
              target_url: shareURL,
              delete_token: result.delete_token || '',
              org: org || '',
              org_name: (orgMeta && orgMeta.name) || '',
              visibility: visibility || '',
            }),
          });
          if (!persistResp.ok) throw new Error('Server error persisting share state ' + persistResp.status);
          const persisted = await persistResp.json().catch(function() { return {}; });
          if (!persisted.hosted_token) {
            console.warn('share: /api/share-url did not return hosted_token');
          }
          hostedToken = persisted.hosted_token || '';
        } else {
          const opts = { method: 'POST', headers: { 'Content-Type': 'application/json' } };
          const shareBody = { target_url: shareURL };
          if (org || visibility) {
            if (org) shareBody.org = org;
            if (visibility) shareBody.visibility = visibility;
            if (orgMeta && orgMeta.name) shareBody.org_name = orgMeta.name;
          }
          opts.body = JSON.stringify(shareBody);
          const resp = await fetch('/api/share', opts);
          if (!resp.ok) {
            const errBody = await resp.json().catch(function() { return {}; });
            throw new Error(errBody.error || 'Server error ' + resp.status);
          }
          result = await resp.json();
          try {
            const cfgResp = await fetch('/api/config');
            if (cfgResp.ok) {
              const cfg = await cfgResp.json();
              hostedToken = cfg.hosted_token || '';
            }
          } catch { /* non-fatal; hostedToken will be empty */ }
        }
        hostedURL = result.url;
        deleteToken = result.delete_token || '';
        sharedOrg = orgMeta || null;
        sharedVisibility = visibility || 'unlisted';
        setShareButtonState('shared');
        showShareModal();
      } catch (err) {
        setShareButtonState('default');
        showShareError(err);
      } finally {
        if (popupSession) popupSession.close();
        shareInFlight = false;
      }
    }

    function showOrgShareModal(orgs, sharePolicy) {
      closeShareModal();
      sharePolicy = normalizeSharePolicy(sharePolicy);
      const overlay = document.createElement('div');
      overlay.className = 'share-overlay';
      overlay.setAttribute('role', 'dialog');
      overlay.setAttribute('aria-modal', 'true');
      overlay.setAttribute('aria-labelledby', 'orgShareTitle');

      const settingSuffix = ':' + shareURL;
      const savedOrg = getSetting('shareOrg' + settingSuffix, '');
      const savedVis = getSetting('shareVisibility' + settingSuffix, '');
      const initials = authUserName
        ? authUserName.split(/\s+/).filter(Boolean).map(function(w) { return w[0]; }).join('').slice(0, 2).toUpperCase()
        : '';

      const ICON_ORG = '<svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor"><path d="M1.75 16A1.75 1.75 0 0 1 0 14.25V1.75C0 .784.784 0 1.75 0h8.5C11.216 0 12 .784 12 1.75v12.5c0 .085-.006.168-.018.25h2.268a.25.25 0 0 0 .25-.25V8.285a.25.25 0 0 0-.111-.208l-1.055-.703a.749.749 0 1 1 .832-1.248l1.055.703c.487.325.777.871.777 1.456v5.965A1.75 1.75 0 0 1 14.25 16h-3.5a.766.766 0 0 1-.197-.026c-.099.017-.2.026-.303.026h-3a.75.75 0 0 1-.75-.75V14h-1v1.25a.75.75 0 0 1-.75.75h-3Zm-.25-1.75c0 .138.112.25.25.25H4v-1.25a.75.75 0 0 1 .75-.75h2.5a.75.75 0 0 1 .75.75v1.25h2.25a.25.25 0 0 0 .25-.25V1.75a.25.25 0 0 0-.25-.25h-8.5a.25.25 0 0 0-.25.25ZM3.75 6h.5a.75.75 0 0 1 0 1.5h-.5a.75.75 0 0 1 0-1.5ZM3 3.75A.75.75 0 0 1 3.75 3h.5a.75.75 0 0 1 0 1.5h-.5A.75.75 0 0 1 3 3.75Zm4 3A.75.75 0 0 1 7.75 6h.5a.75.75 0 0 1 0 1.5h-.5A.75.75 0 0 1 7 6.75ZM7.75 3h.5a.75.75 0 0 1 0 1.5h-.5a.75.75 0 0 1 0-1.5ZM3 9.75A.75.75 0 0 1 3.75 9h.5a.75.75 0 0 1 0 1.5h-.5A.75.75 0 0 1 3 9.75ZM7.75 9h.5a.75.75 0 0 1 0 1.5h-.5a.75.75 0 0 1 0-1.5Z"/></svg>';
      const ICON_VIS_ORG = '<svg class="sd-org-vis-icon" width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M1.75 16A1.75 1.75 0 0 1 0 14.25V1.75C0 .784.784 0 1.75 0h8.5C11.216 0 12 .784 12 1.75v12.5c0 .085-.006.168-.018.25h2.268a.25.25 0 0 0 .25-.25V8.285a.25.25 0 0 0-.111-.208l-1.055-.703a.749.749 0 1 1 .832-1.248l1.055.703c.487.325.777.871.777 1.456v5.965A1.75 1.75 0 0 1 14.25 16h-3.5a.766.766 0 0 1-.197-.026c-.099.017-.2.026-.303.026h-3a.75.75 0 0 1-.75-.75V14h-1v1.25a.75.75 0 0 1-.75.75h-3Zm-.25-1.75c0 .138.112.25.25.25H4v-1.25a.75.75 0 0 1 .75-.75h2.5a.75.75 0 0 1 .75.75v1.25h2.25a.25.25 0 0 0 .25-.25V1.75a.25.25 0 0 0-.25-.25h-8.5a.25.25 0 0 0-.25.25ZM3.75 6h.5a.75.75 0 0 1 0 1.5h-.5a.75.75 0 0 1 0-1.5ZM3 3.75A.75.75 0 0 1 3.75 3h.5a.75.75 0 0 1 0 1.5h-.5A.75.75 0 0 1 3 3.75Zm4 3A.75.75 0 0 1 7.75 6h.5a.75.75 0 0 1 0 1.5h-.5A.75.75 0 0 1 7 6.75ZM7.75 3h.5a.75.75 0 0 1 0 1.5h-.5a.75.75 0 0 1 0-1.5ZM3 9.75A.75.75 0 0 1 3.75 9h.5a.75.75 0 0 1 0 1.5h-.5A.75.75 0 0 1 3 9.75ZM7.75 9h.5a.75.75 0 0 1 0 1.5h-.5a.75.75 0 0 1 0-1.5Z"/></svg>';
      const ICON_VIS_UNLISTED = '<svg class="sd-org-vis-icon" width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M8 2c1.981 0 3.671.992 4.933 2.078 1.27 1.091 2.187 2.345 2.637 3.023a1.62 1.62 0 0 1 0 1.798c-.45.678-1.367 1.932-2.637 3.023C11.67 13.008 9.981 14 8 14s-3.671-.992-4.933-2.078C1.797 10.831.88 9.577.43 8.9a1.619 1.619 0 0 1 0-1.798c.45-.678 1.367-1.932 2.637-3.023C4.33 2.992 6.019 2 8 2ZM1.679 7.932a.12.12 0 0 0 0 .136c.411.622 1.241 1.75 2.366 2.717C5.176 11.758 6.527 12.5 8 12.5s2.825-.742 3.955-1.715c1.124-.967 1.954-2.096 2.366-2.717a.12.12 0 0 0 0-.136c-.412-.621-1.242-1.75-2.366-2.717C10.824 4.242 9.473 3.5 8 3.5S5.176 4.242 4.045 5.215C2.92 6.182 2.09 7.311 1.679 7.932ZM8 10a2 2 0 1 1-.001-3.999A2 2 0 0 1 8 10Z"/></svg>';
      const ICON_VIS_PUBLIC = '<svg class="sd-org-vis-icon" width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0ZM1.5 8a6.5 6.5 0 1 0 13 0 6.5 6.5 0 0 0-13 0Z"/></svg>';

      const isPersonalSelected = !savedOrg || !orgs.some(function(o) { return o.slug === savedOrg; });
      const initialPersonalVis = sharePolicyAllowsVisibility(sharePolicy, 'unlisted') ? 'unlisted' : sharePolicy.allowed_review_visibilities[0];
      const initialOrgVis = sharePolicyAllowsVisibility(sharePolicy, 'organization') ? 'organization' : sharePolicy.allowed_review_visibilities[0];

      // Build owner rows using sd-org-owner-option (custom radio, no native input)
      let ownerRows = '';
      ownerRows +=
        '<div class="sd-org-owner-option" role="radio" aria-checked="' + isPersonalSelected + '" tabindex="' + (isPersonalSelected ? '0' : '-1') + '" data-owner="" data-default-vis="' + escapeHtml(initialPersonalVis) + '">' +
          '<span class="sd-org-radio"><span class="sd-org-radio-dot"></span></span>' +
          '<span class="sd-org-avatar sd-org-avatar--personal">' + escapeHtml(initials || '?') + '</span>' +
          '<span class="sd-org-owner-info"><span class="sd-org-owner-name">' + escapeHtml(authUserName || 'Personal') + '</span><span class="sd-org-owner-slug">Personal</span></span>' +
        '</div>';
      for (let oi = 0; oi < orgs.length; oi++) {
        const org = orgs[oi];
        const isSelected = savedOrg === org.slug;
        ownerRows +=
          '<div class="sd-org-owner-option" role="radio" aria-checked="' + isSelected + '" tabindex="' + (isSelected ? '0' : '-1') + '" data-owner="' + escapeHtml(org.slug) + '" data-default-vis="' + escapeHtml(initialOrgVis) + '">' +
            '<span class="sd-org-radio"><span class="sd-org-radio-dot"></span></span>' +
            '<span class="sd-org-avatar sd-org-avatar--org">' + ICON_ORG + '</span>' +
            '<span class="sd-org-owner-info"><span class="sd-org-owner-name">' + escapeHtml(org.name) + '</span><span class="sd-org-owner-slug">' + escapeHtml(org.slug) + '</span></span>' +
          '</div>';
      }

      overlay.innerHTML =
        '<div class="share-dialog sd-org-dialog">' +
          '<div class="sd-org-header">' +
            '<h3 id="orgShareTitle" class="sd-org-title">Share review</h3>' +
            '<button class="sd-org-close" aria-label="Close"><svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor"><path d="M3.72 3.72a.75.75 0 0 1 1.06 0L8 6.94l3.22-3.22a.75.75 0 1 1 1.06 1.06L9.06 8l3.22 3.22a.75.75 0 1 1-1.06 1.06L8 9.06l-3.22 3.22a.75.75 0 0 1-1.06-1.06L6.94 8 3.72 4.78a.75.75 0 0 1 0-1.06z"/></svg></button>' +
          '</div>' +
          '<div class="sd-org-body">' +
            '<div>' +
              '<label class="sd-org-label" id="orgOwnerLabel">Owner</label>' +
              '<div class="sd-org-owner-list" role="radiogroup" aria-labelledby="orgOwnerLabel">' + ownerRows + '</div>' +
            '</div>' +
            '<div>' +
              '<label class="sd-org-label" id="orgVisLabel">Visibility</label>' +
              '<div class="sd-org-vis-options" role="radiogroup" aria-labelledby="orgVisLabel" id="orgVisOptions">' +
                '<div class="sd-org-vis-option" role="radio" aria-checked="false" tabindex="-1" data-vis="organization" style="display:none">' +
                  '<span class="sd-org-radio"><span class="sd-org-radio-dot"></span></span>' +
                  ICON_VIS_ORG +
                  '<span class="sd-org-vis-text"><span class="sd-org-vis-label">Organization</span><span class="sd-org-vis-desc" id="orgVisOrgDesc">Only members can view</span></span>' +
                '</div>' +
                '<div class="sd-org-vis-option" role="radio" aria-checked="false" tabindex="-1" data-vis="unlisted">' +
                  '<span class="sd-org-radio"><span class="sd-org-radio-dot"></span></span>' +
                  ICON_VIS_UNLISTED +
                  '<span class="sd-org-vis-text"><span class="sd-org-vis-label">Unlisted</span><span class="sd-org-vis-desc" id="orgVisUnlistedDesc">Anyone with the link can view</span></span>' +
                '</div>' +
                '<div class="sd-org-vis-option" role="radio" aria-checked="false" tabindex="-1" data-vis="public">' +
                  '<span class="sd-org-radio"><span class="sd-org-radio-dot"></span></span>' +
                  ICON_VIS_PUBLIC +
                  '<span class="sd-org-vis-text"><span class="sd-org-vis-label">Public</span><span class="sd-org-vis-desc">Discoverable by anyone on the web</span></span>' +
                '</div>' +
              '</div>' +
            '</div>' +
            '<label class="sd-org-remember"><input type="checkbox" id="orgRememberCheck" /><span class="sd-org-remember-text">Remember my choice</span></label>' +
          '</div>' +
          '<div class="sd-org-footer">' +
            '<span class="sd-org-consent">Uploads to <a href="' + escapeHtml(shareURL) + '" target="_blank" rel="noopener">' + escapeHtml(shareURL.replace(/^https?:\/\//, '')) + '</a></span>' +
            '<div class="sd-org-footer-actions">' +
              '<button class="sd-org-btn-cancel" id="orgCancelBtn">Cancel</button>' +
              '<button class="sd-org-btn-share" id="orgShareBtn">Share</button>' +
            '</div>' +
          '</div>' +
        '</div>';

      document.body.appendChild(overlay);
      shareModalEl = overlay;

      const ownerList = overlay.querySelector('.sd-org-owner-list');
      const visOptions = overlay.querySelector('#orgVisOptions');
      const orgVisOption = visOptions.querySelector('[data-vis="organization"]');
      const orgVisDesc = overlay.querySelector('#orgVisOrgDesc');
      const unlistedVisDesc = overlay.querySelector('#orgVisUnlistedDesc');
      const shareButton = overlay.querySelector('#orgShareBtn');

      function isVisAvailable(el) {
        return !!el && el.style.display !== 'none' && el.getAttribute('aria-disabled') !== 'true';
      }

      function firstAvailableVis() {
        return Array.from(visOptions.querySelectorAll('.sd-org-vis-option')).find(isVisAvailable) || null;
      }

      function applyPolicyToVisOptions() {
        visOptions.querySelectorAll('.sd-org-vis-option').forEach(function(el) {
          const visibility = el.getAttribute('data-vis');
          const allowed = sharePolicyAllowsVisibility(sharePolicy, visibility);
          el.setAttribute('aria-disabled', allowed ? 'false' : 'true');
          if (!allowed) {
            el.setAttribute('tabindex', '-1');
            el.setAttribute('title', 'Disabled by this Crit instance');
          } else {
            el.removeAttribute('title');
          }
        });
      }

      function syncShareButtonState() {
        if (shareButton) shareButton.disabled = !firstAvailableVis();
      }

      function selectOwner(el) {
        ownerList.querySelectorAll('.sd-org-owner-option').forEach(function(o) {
          o.setAttribute('aria-checked', 'false');
          o.setAttribute('tabindex', '-1');
        });
        el.setAttribute('aria-checked', 'true');
        el.setAttribute('tabindex', '0');
        el.focus();
        const owner = el.getAttribute('data-owner');
        const orgName = el.querySelector('.sd-org-owner-name').textContent;
        if (owner) {
          orgVisOption.style.display = '';
          orgVisDesc.textContent = 'Only members of ' + orgName + ' can view';
          unlistedVisDesc.textContent = 'Anyone with the link at ' + orgName + ' can view';
        } else {
          orgVisOption.style.display = 'none';
          unlistedVisDesc.textContent = 'Anyone with the link can view';
          if (orgVisOption.getAttribute('aria-checked') === 'true') {
            selectVis(visOptions.querySelector('[data-vis="unlisted"]'));
          }
        }
        const defaultVis = visOptions.querySelector('[data-vis="' + el.getAttribute('data-default-vis') + '"]');
        selectVis(isVisAvailable(defaultVis) ? defaultVis : firstAvailableVis());
        syncShareButtonState();
      }

      function selectVis(el) {
        if (!isVisAvailable(el)) return;
        visOptions.querySelectorAll('.sd-org-vis-option').forEach(function(o) {
          o.setAttribute('aria-checked', 'false');
          o.setAttribute('tabindex', '-1');
        });
        el.setAttribute('aria-checked', 'true');
        el.setAttribute('tabindex', '0');
      }

      function radioKeyNav(container, selector, selectFn) {
        container.addEventListener('keydown', function(e) {
          const items = Array.from(container.querySelectorAll(selector)).filter(function(o) {
            return o.style.display !== 'none' && o.getAttribute('aria-disabled') !== 'true';
          });
          if (items.length === 0) return;
          const current = e.target.closest(selector);
          if (!current) return;
          const idx = items.indexOf(current);
          if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
            e.preventDefault();
            selectFn(items[(idx + 1) % items.length]);
          } else if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
            e.preventDefault();
            selectFn(items[(idx - 1 + items.length) % items.length]);
          }
        });
      }

      ownerList.addEventListener('click', function(e) {
        const opt = e.target.closest('.sd-org-owner-option');
        if (opt) selectOwner(opt);
      });
      radioKeyNav(ownerList, '.sd-org-owner-option', selectOwner);

      visOptions.addEventListener('click', function(e) {
        const opt = e.target.closest('.sd-org-vis-option');
        if (isVisAvailable(opt)) { selectVis(opt); opt.focus(); }
      });
      radioKeyNav(visOptions, '.sd-org-vis-option', function(el) { selectVis(el); el.focus(); });

      // Apply initial selection
      applyPolicyToVisOptions();
      const initialOwner = ownerList.querySelector('[aria-checked="true"]');
      if (initialOwner) {
        const owner = initialOwner.getAttribute('data-owner');
        if (owner) {
          const initialOrgName = initialOwner.querySelector('.sd-org-owner-name').textContent;
          orgVisOption.style.display = '';
          orgVisDesc.textContent = 'Only members of ' + initialOrgName + ' can view';
          unlistedVisDesc.textContent = 'Anyone with the link at ' + initialOrgName + ' can view';
        }
        const defVis = savedVis || initialOwner.getAttribute('data-default-vis');
        const visEl = visOptions.querySelector('[data-vis="' + defVis + '"]');
        if (isVisAvailable(visEl)) {
          selectVis(visEl);
        } else {
          const defaultVis = visOptions.querySelector('[data-vis="' + initialOwner.getAttribute('data-default-vis') + '"]');
          selectVis(isVisAvailable(defaultVis) ? defaultVis : firstAvailableVis());
        }
      }
      syncShareButtonState();

      // Close handlers
      overlay.addEventListener('click', function(e) { if (e.target === overlay) closeShareModal(); });
      overlay.addEventListener('keydown', function(e) { if (e.key === 'Escape') closeShareModal(); });
      overlay.querySelector('.sd-org-close').addEventListener('click', closeShareModal);
      overlay.querySelector('#orgCancelBtn').addEventListener('click', closeShareModal);

      // Share handler
      overlay.querySelector('#orgShareBtn').addEventListener('click', async function() {
        const btn = this;
        btn.disabled = true;

        const selectedOwnerEl = ownerList.querySelector('[aria-checked="true"]');
        const selectedVisEl = visOptions.querySelector('[aria-checked="true"]');
        const orgSlug = selectedOwnerEl ? selectedOwnerEl.getAttribute('data-owner') : '';
        const visibility = selectedVisEl ? selectedVisEl.getAttribute('data-vis') : 'unlisted';
        const remember = overlay.querySelector('#orgRememberCheck').checked;

        if (remember) {
          setSetting('shareOrg' + settingSuffix, orgSlug);
          setSetting('shareVisibility' + settingSuffix, visibility);
        }

        const orgMeta = orgSlug && selectedOwnerEl
          ? { slug: orgSlug, name: selectedOwnerEl.querySelector('.sd-org-owner-name').textContent }
          : null;

        closeShareModal();

        // Open popup synchronously BEFORE any await — Safari blocks popups
        // after async gaps.
        let popupSession = null;
        if (proxyAuth) {
          try { popupSession = openShareReceiver(shareURL); }
          catch (err) { btn.disabled = false; showShareError(err); return; }
        }

        if (needsShareConsent) {
          try {
            const cr = await fetch('/api/share-consent', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ target_url: shareURL }) });
            if (cr.ok) {
              needsShareConsent = false;
            } else {
              btn.disabled = false;
              if (popupSession) popupSession.close();
              showToast('share', 'error', '<span>Failed to record consent. Please try again.</span>');
              return;
            }
          } catch {
            btn.disabled = false;
            if (popupSession) popupSession.close();
            showToast('share', 'error', '<span>Network error. Please try again.</span>');
            return;
          }
        }

        performShare(orgSlug, visibility, orgMeta, popupSession);
      });

      requestAnimationFrame(function() {
        const btn = overlay.querySelector('#orgShareBtn');
        if (btn) btn.focus();
      });
    }

    function showConsentModal() {
      closeShareModal();
      const overlay = document.createElement('div');
      overlay.className = 'share-overlay';
      overlay.setAttribute('role', 'dialog');
      overlay.setAttribute('aria-modal', 'true');
      overlay.setAttribute('aria-labelledby', 'consentDialogTitle');
      overlay.innerHTML =
        '<div class="share-dialog share-dialog--consent">' +
          '<h3 id="consentDialogTitle" class="share-dialog-headline">Share this review</h3>' +
          '<p class="share-dialog-sub">Your review will be securely uploaded to crit.md. ' +
            'You\'ll get a private link — share it with whoever you choose. ' +
            'You won\'t be asked again after confirming.</p>' +
          '<div class="sd-actions">' +
            '<button class="sd-link-btn" id="consentCancelBtn">Cancel</button>' +
            '<button class="sd-primary" id="consentShareBtn">Share →</button>' +
          '</div>' +
        '</div>';
      document.body.appendChild(overlay);
      shareModalEl = overlay;
      requestAnimationFrame(function() {
        const focusBtn = overlay.querySelector('#consentShareBtn');
        if (focusBtn) focusBtn.focus();
      });

      let consentAborted = false;
      overlay.addEventListener('click', function(e) { if (e.target === overlay) { consentAborted = true; closeShareModal(); } });
      overlay.addEventListener('keydown', function(e) { if (e.key === 'Escape') { consentAborted = true; closeShareModal(); } });
      overlay.querySelector('#consentCancelBtn').addEventListener('click', function() { consentAborted = true; closeShareModal(); });
      overlay.querySelector('#consentShareBtn').addEventListener('click', async function() {
        this.disabled = true;
        try {
          const r = await fetch('/api/share-consent', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ target_url: shareURL }) });
          if (r.ok) {
            needsShareConsent = false;
            if (selectedTarget) selectedTarget.needs_share_consent = false;
            const continueTarget = selectedTarget;
            closeShareModal();
            if (!consentAborted && continueTarget) {
              beginShareToTarget(continueTarget);
            }
          } else {
            closeShareModal();
            showToast('share', 'error', '<span>Failed to record consent. Please try again.</span>');
          }
        } catch {
          closeShareModal();
          showToast('share', 'error', '<span>Network error. Please try again.</span>');
        }
      });
    }

    function showShareModal() {
      closeShareModal();
      const overlay = document.createElement('div');
      overlay.className = 'share-overlay';
      overlay.setAttribute('role', 'dialog');
      overlay.setAttribute('aria-modal', 'true');
      overlay.setAttribute('aria-labelledby', 'shareDialogTitle');

      const isSignedIn = !!authUserName;
      const initials = authUserName
        ? authUserName.split(/\s+/).filter(Boolean).map(function(w) { return w[0]; }).join('').slice(0, 2).toUpperCase()
        : '';

      const nextShareBlock = isSignedIn
        ? '<div class="share-dialog-attrib">' +
            '<span class="share-dialog-avatar" aria-hidden="true">' + escapeHtml(initials) + '</span>' +
            '<span>Shared as <strong>' + escapeHtml(authUserName) + '</strong></span>' +
          '</div>'
        : '<div class="share-dialog-next">' +
            '<span class="share-dialog-next-eyebrow">For your next share</span>' +
            '<p class="share-dialog-next-body">Sign in once from your terminal and every review you share ' +
              'after that will be attributed to you and listed in your dashboard.</p>' +
            '<div class="share-dialog-cmd">' +
              '<span class="share-dialog-cmd-prompt" aria-hidden="true">$</span>' +
              '<span class="share-dialog-cmd-text">crit auth login</span>' +
              '<button class="share-dialog-cmd-copy" id="modalCopyCmd" aria-label="Copy command">' +
                ICON_CLIPBOARD +
              '</button>' +
            '</div>' +
          '</div>';

      let subtitleText = 'Anyone with the link can read it. The page works without an account.';
	  if (!shareURL && shareBaseURL) subtitleText = 'The originating instance (' + escapeHtml(shareBaseURL) + ') is no longer configured. The existing link is preserved.';
      const missingOrigin = !shareURL && !!shareBaseURL;
      let orgStripHtml = '';
      if (sharedOrg) {
        const orgName = escapeHtml(sharedOrg.name);
        const vis = sharedVisibility || 'unlisted';
        const ICON_BUILDING_SM = '<svg class="sd-shared-org-icon" viewBox="0 0 16 16" fill="currentColor"><path d="M1.75 1A1.75 1.75 0 000 2.75v10.5C0 14.216.784 15 1.75 15h4.5a.75.75 0 00.75-.75v-3.5a.25.25 0 01.25-.25h1.5a.25.25 0 01.25.25v3.5c0 .414.336.75.75.75h4.5A1.75 1.75 0 0016 13.25V2.75A1.75 1.75 0 0014.25 1H1.75zm.25 1.75a.25.25 0 01.25-.25h11.5a.25.25 0 01.25.25v10.5a.25.25 0 01-.25.25H10v-2.75A1.75 1.75 0 008.25 9h-1.5A1.75 1.75 0 005 10.75V14H2.25a.25.25 0 01-.25-.25V2.75zM4 4a1 1 0 011-1h1a1 1 0 010 2H5a1 1 0 01-1-1zm6-1a1 1 0 100 2h1a1 1 0 100-2h-1zM4 7a1 1 0 011-1h1a1 1 0 110 2H5a1 1 0 01-1-1zm6-1a1 1 0 100 2h1a1 1 0 100-2h-1z"/></svg>';
        let pillIcon = '';
        const pillClass = 'sd-shared-vis-pill--' + vis;
        let pillLabel = '';
        let hintText = '';
        if (vis === 'organization') {
          subtitleText = 'Only ' + orgName + ' members can view this review.';
          pillIcon = '<svg viewBox="0 0 16 16" fill="currentColor"><path fill-rule="evenodd" d="M7.467.133a1.75 1.75 0 011.066 0l5.25 1.68A1.75 1.75 0 0115 3.48V7c0 1.566-.32 3.182-1.303 4.682-.983 1.498-2.585 2.813-5.032 3.855a1.7 1.7 0 01-1.33 0c-2.447-1.042-4.049-2.357-5.032-3.855C1.32 10.182 1 8.566 1 7V3.48a1.75 1.75 0 011.217-1.667l5.25-1.68zm.61 1.429a.25.25 0 00-.153 0l-5.25 1.68a.25.25 0 00-.174.238V7c0 1.358.275 2.666 1.057 3.86.784 1.194 2.121 2.34 4.366 3.297a.2.2 0 00.154 0c2.245-.956 3.582-2.103 4.366-3.298C13.225 9.666 13.5 8.358 13.5 7V3.48a.25.25 0 00-.174-.238l-5.25-1.68z"/></svg>';
          pillLabel = 'Members only';
        } else if (vis === 'unlisted') {
          subtitleText = 'Anyone at ' + orgName + ' with the link can view this review.';
          pillIcon = '<svg viewBox="0 0 16 16" fill="currentColor"><path d="M7.775 3.275a.75.75 0 001.06 1.06l1.25-1.25a2 2 0 112.83 2.83l-2.5 2.5a2 2 0 01-2.83 0 .75.75 0 00-1.06 1.06 3.5 3.5 0 004.95 0l2.5-2.5a3.5 3.5 0 00-4.95-4.95l-1.25 1.25zm-4.69 9.64a2 2 0 010-2.83l2.5-2.5a2 2 0 012.83 0 .75.75 0 001.06-1.06 3.5 3.5 0 00-4.95 0l-2.5 2.5a3.5 3.5 0 004.95 4.95l1.25-1.25a.75.75 0 00-1.06-1.06l-1.25 1.25a2 2 0 01-2.83 0z"/></svg>';
          pillLabel = 'Unlisted';
          hintText = 'Anyone at org with link';
        } else if (vis === 'public') {
          subtitleText = 'This review is discoverable by anyone on the web.';
          pillIcon = '<svg viewBox="0 0 16 16" fill="currentColor"><path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0ZM1.5 8a6.5 6.5 0 1 0 13 0 6.5 6.5 0 0 0-13 0Z"/></svg>';
          pillLabel = 'Public';
          hintText = 'Discoverable by anyone';
        }
        orgStripHtml =
          '<div class="sd-shared-org-strip">' +
            ICON_BUILDING_SM +
            '<span class="sd-shared-org-name">' + orgName + '</span>' +
            '<span class="sd-shared-org-sep">&middot;</span>' +
            '<span class="sd-shared-vis-pill ' + pillClass + '">' + pillIcon + pillLabel + '</span>' +
            (hintText ? '<span class="sd-shared-vis-hint">' + hintText + '</span>' : '') +
          '</div>';
      }

      overlay.innerHTML =
        '<div class="share-dialog">' +
          '<div class="share-dialog-body">' +
            '<div class="share-dialog-qr-col">' +
              '<div class="share-dialog-qr" id="modalQR"></div>' +
              '<div class="share-dialog-qr-caption">Scan to open on a phone</div>' +
            '</div>' +
            '<div class="share-dialog-narrative">' +
              '<h3 id="shareDialogTitle" class="share-dialog-headline">Your review is live.</h3>' +
              '<p class="share-dialog-sub">' + subtitleText + '</p>' +
              '<div class="share-dialog-url">' +
                '<span>' + escapeHtml(hostedURL) + '</span>' +
                '<button class="copy-icon-btn" id="modalCopyBtn" aria-label="Copy link">' +
                  ICON_CLIPBOARD +
                '</button>' +
              '</div>' +
              nextShareBlock +
              orgStripHtml +
            '</div>' +
          '</div>' +
          '<div class="sd-actions">' +
            (deleteToken || missingOrigin
              ? '<button class="sd-link-btn sd-link-btn--danger" id="modalUnpublishBtn">' +
                (missingOrigin ? 'Clear local link' : 'Unpublish') +
                '</button>'
              : '<span></span>') +
            '<div class="sd-actions-right">' +
              (missingOrigin
                ? ''
                : '<button class="sd-link-btn" id="modalPullBtn">Pull comments</button>' +
                  '<button class="sd-link-btn" id="modalReshareBtn">Re-share</button>') +
              '<button class="sd-primary" id="modalCloseBtn">Done</button>' +
            '</div>' +
          '</div>' +
        '</div>';

      document.body.appendChild(overlay);
      shareModalEl = overlay;
      requestAnimationFrame(function() {
        const closeBtn = overlay.querySelector('#modalCloseBtn');
        if (closeBtn) closeBtn.focus();
      });

      // QR code
      fetch('/api/qr?url=' + encodeURIComponent(hostedURL))
        .then(function(r) { return r.ok ? r.text() : null; })
        .then(function(svg) {
          const qrEl = document.getElementById('modalQR');
          if (qrEl && svg) qrEl.innerHTML = svg;
        })
        .catch(function() { /* QR is optional */ });

      // Close on backdrop or Escape
      overlay.addEventListener('click', function(e) { if (e.target === overlay) closeShareModal(); });
      overlay.addEventListener('keydown', function(e) { if (e.key === 'Escape') closeShareModal(); });

      // Copy URL
      overlay.querySelector('#modalCopyBtn').addEventListener('click', function() {
        navigator.clipboard.writeText(hostedURL).catch(function() { /* best-effort */ });
        this.innerHTML = ICON_CHECK_SMALL;
        this.setAttribute('aria-label', 'Copied');
        announceCopy();
        const btn = this;
        setTimeout(function() {
          btn.innerHTML = ICON_CLIPBOARD;
          btn.setAttribute('aria-label', 'Copy link');
        }, 2000);
      });

      // Copy command (anonymous only)
      const copyCmdBtn = overlay.querySelector('#modalCopyCmd');
      if (copyCmdBtn) {
        copyCmdBtn.addEventListener('click', function() {
          navigator.clipboard.writeText('crit auth login').catch(function() { /* best-effort */ });
          this.innerHTML = ICON_CHECK_SMALL;
          this.setAttribute('aria-label', 'Copied');
          const btn = this;
          setTimeout(function() {
            btn.innerHTML = ICON_CLIPBOARD;
            btn.setAttribute('aria-label', 'Copy command');
          }, 2000);
        });
      }

      // Done button
      overlay.querySelector('#modalCloseBtn').addEventListener('click', closeShareModal);

      // Unpublish / clear local binding
      if (deleteToken || missingOrigin) {
        overlay.querySelector('#modalUnpublishBtn').addEventListener('click', missingOrigin ? handleClearLocalShare : showUnpublishConfirm);
      }

      const pullBtn = overlay.querySelector('#modalPullBtn');
      if (pullBtn) pullBtn.addEventListener('click', handlePullComments);
      const reshareBtn = overlay.querySelector('#modalReshareBtn');
      if (reshareBtn) reshareBtn.addEventListener('click', handleReshare);
    }

    // Pull remote comments. Direct transport goes through the local Go server
    // (bearer token attached server-side) so the browser never hits crit-web
    // cross-origin. Proxy-auth still uses the popup relay.
    async function handlePullComments() {
      const btn = document.getElementById('modalPullBtn');
      if (!btn) return;
	  if (!shareURL) { showShareError(new Error('originating Crit instance is no longer configured')); return; }
      btn.disabled = true;
      const origLabel = btn.textContent;
      btn.textContent = 'Pulling…';

      // Open popup synchronously inside the click handler.
      let popupSession = null;
      if (proxyAuth) {
        try {
          popupSession = openShareReceiver(shareURL);
        } catch (err) {
          btn.disabled = false;
          btn.textContent = origLabel;
          showToast('share', 'error', '<span>Pull failed: ' + escapeHtml(err.message) + '</span>');
          return;
        }
      }

      try {
        if (!hostedToken) throw new Error('no shared review token (try re-sharing)');

        if (popupSession) {
          const comments = await popupSession.run('fetch', { token: hostedToken });
          const mergeResp = await fetch('/api/comments/merge', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ comments: comments }),
          });
          if (!mergeResp.ok) {
            const errBody = await mergeResp.json().catch(function() { return {}; });
            throw new Error(errBody.error || 'merge failed: ' + mergeResp.status);
          }
        } else {
          const resp = await fetch('/api/share/pull', { method: 'POST' });
          if (!resp.ok) {
            const errBody = await resp.json().catch(function() { return {}; });
            throw new Error(errBody.error || 'Server error ' + resp.status);
          }
        }

        await onCommentsRefreshed();
        showToast('share', 'success', '<span>Comments pulled</span>', { autoDismiss: true });
      } catch (err) {
        showToast('share', 'error', '<span>Pull failed: ' + escapeHtml(err.message) + '</span>');
      } finally {
        if (popupSession) popupSession.close();
        const liveBtn = document.getElementById('modalPullBtn');
        if (liveBtn) {
          liveBtn.disabled = false;
          liveBtn.textContent = origLabel;
        }
      }
    }

    // Re-share: pull -> merge -> upsert. Direct transport is entirely
    // server-side (/api/share/reshare) so the browser never calls crit-web
    // cross-origin. Proxy-auth keeps the popup relay for the same API calls.
    async function handleReshare() {
      const btn = document.getElementById('modalReshareBtn');
      if (!btn) return;
	  if (!shareURL) { showShareError(new Error('originating Crit instance is no longer configured')); return; }
      btn.disabled = true;
      const origLabel = btn.textContent;
      btn.textContent = 'Re-sharing…';

      // Open popup synchronously inside the click handler.
      let popupSession = null;
      if (proxyAuth) {
        try {
          popupSession = openShareReceiver(shareURL);
        } catch (err) {
          btn.disabled = false;
          btn.textContent = origLabel;
          showToast('share', 'error', '<span>Re-share failed: ' + escapeHtml(err.message) + '</span>');
          return;
        }
      }

      try {
        if (!hostedToken) throw new Error('no shared review token (try re-sharing)');

        if (popupSession) {
          // 1. Pull remote comments through the (still authenticated) popup.
          const remoteComments = await popupSession.run('fetch', { token: hostedToken });

          // 2. Merge locally. NOTE: this mutates the local review file even if
          // the upsert in step 4 fails — the toast in the catch block documents
          // the partial-failure recovery path.
          const mergeResp = await fetch('/api/comments/merge', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ comments: remoteComments }),
          });
          if (!mergeResp.ok) {
            const errBody = await mergeResp.json().catch(function() { return {}; });
            throw new Error(errBody.error || 'merge failed: ' + mergeResp.status);
          }

          // 3. Build upsert payload from the merged local state.
          const payloadResp = await fetch('/api/share/upsert-payload');
          if (!payloadResp.ok) {
            const errBody = await payloadResp.json().catch(function() { return {}; });
            throw new Error(errBody.error || 'failed to build upsert payload');
          }
          const payload = await payloadResp.json();

          // 4. PUT through the same popup session (still authenticated).
          await popupSession.run('upsert', { token: hostedToken, payload: payload });
        } else {
          const resp = await fetch('/api/share/reshare', { method: 'POST' });
          if (!resp.ok) {
            const errBody = await resp.json().catch(function() { return {}; });
            throw new Error(errBody.error || 'Server error ' + resp.status);
          }
        }

        const bindResp = await fetch('/api/share-url', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url: hostedURL, target_url: shareURL, delete_token: deleteToken }),
        });
        if (!bindResp.ok) throw new Error('re-share succeeded but target binding could not be saved');

        await onCommentsRefreshed();
        showToast('share', 'success', '<span>Re-shared</span>', { autoDismiss: true });
      } catch (err) {
        // Document partial-failure state explicitly. If the merge succeeded
        // but the upsert failed, the user's local review file already contains
        // the remote comments — clicking Re-share again will retry the upsert
        // (idempotent on the server) but may require re-authentication if the
        // popup was closed.
        showToast('share', 'error',
          '<span>Re-share failed: ' + escapeHtml(err.message) +
          '. Local comments may have been updated; click Re-share again to retry' +
          (proxyAuth ? ' (you may need to re-authenticate in the popup)' : '') +
          '.</span>');
      } finally {
        if (popupSession) popupSession.close();
        const liveBtn = document.getElementById('modalReshareBtn');
        if (liveBtn) {
          liveBtn.disabled = false;
          liveBtn.textContent = origLabel;
        }
      }
    }

    function showUnpublishConfirm() {
      if (!shareModalEl) return;
      const dialog = shareModalEl.querySelector('.share-dialog');
      shareModalEl.setAttribute('aria-labelledby', 'unpublishDialogTitle');
      dialog.innerHTML =
        '<div class="share-dialog-confirm">' +
          '<p id="unpublishDialogTitle">Unpublish this review?</p>' +
          '<p class="confirm-detail">The shared link will stop working. Comments added by viewers will be lost.</p>' +
          '<div class="confirm-actions">' +
            '<button class="btn btn-sm btn-danger" id="confirmUnpublishBtn">Unpublish</button>' +
            '<button class="btn btn-sm" id="cancelUnpublishBtn">Cancel</button>' +
          '</div>' +
        '</div>';
      dialog.querySelector('#confirmUnpublishBtn').addEventListener('click', handleUnpublish);
      dialog.querySelector('#cancelUnpublishBtn').addEventListener('click', showShareModal);
    }

    async function handleClearLocalShare() {
      try {
        const clearResp = await fetch('/api/share-url?local_only=1', { method: 'DELETE' });
        if (!clearResp.ok && clearResp.status !== 204) {
          throw new Error('failed to clear local share state');
        }
        hostedURL = '';
        hostedToken = '';
        deleteToken = '';
        shareBaseURL = '';
        selectTarget(shareTargets.find(function(t) { return t.default; }) || (shareTargets.length === 1 ? shareTargets[0] : null));
        setShareButtonState('default');
        closeShareModal();
        showToast('share', 'success', '<span>Local share link cleared</span>');
      } catch (err) {
        closeShareModal();
        showShareError(err);
      }
    }

    async function handleUnpublish() {
	  if (!shareURL) { closeShareModal(); showShareError(new Error('originating Crit instance is no longer configured')); return; }
      const btn = document.getElementById('confirmUnpublishBtn');
      if (btn) { btn.textContent = 'Unpublishing…'; btn.disabled = true; }

      // Popup-relay path: open the popup synchronously inside the click chain.
      // handleUnpublish is wired via addEventListener in showUnpublishConfirm,
      // so this function is still inside the user-gesture tick when the user
      // clicks "Unpublish" in the confirm dialog.
      let popupSession = null;
      if (proxyAuth) {
        try {
          popupSession = openShareReceiver(shareURL);
        } catch (err) {
          closeShareModal();
          showUnpublishError(err);
          return;
        }
      }

      try {
        if (popupSession) {
          // Receiver normalises 404 (already deleted) to {already_deleted: true}.
          await popupSession.run('unpublish', { delete_token: deleteToken });
          // Remote delete already happened via the popup; only clear local
          // session state. A full DELETE /api/share-url would re-contact
          // crit-web from Go and fail behind the SSO proxy.
          const clearResp = await fetch('/api/share-url?local_only=1', { method: 'DELETE' });
          if (!clearResp.ok && clearResp.status !== 204) {
            console.warn('share: failed to clear local share-url state', clearResp.status);
          }
        } else {
          // Direct transport: DELETE /api/share-url unpublisheds remotely
          // (bearer attached server-side) and clears local session state.
          // Do NOT call crit-web cross-origin — that fails CORS on selfhosted
          // OAuth instances and is redundant with this endpoint.
          const resp = await fetch('/api/share-url', { method: 'DELETE' });
          if (!resp.ok && resp.status !== 204) {
            const errBody = await resp.json().catch(function() { return {}; });
            throw new Error(errBody.error || 'Server error ' + resp.status);
          }
        }
        hostedURL = '';
        deleteToken = '';
        hostedToken = '';
        sharedOrg = null;
        sharedVisibility = '';
        closeShareModal();
        setShareButtonState('default');
      } catch (err) {
        closeShareModal();
        showUnpublishError(err);
      } finally {
        if (popupSession) popupSession.close();
      }
    }

    function showUnpublishError(err) {
      const el = showToast('share', 'error',
        '<span>Unpublish failed: ' + escapeHtml(err.message) + '</span>' +
        '<div class="toast-actions">' +
          '<button class="toast-btn toast-btn-filled" id="shareUnpublishRetryBtn">Retry</button>' +
          '<button class="toast-btn toast-btn-ghost" data-dismiss-toast="share">Dismiss</button>' +
        '</div>');
      el.querySelector('#shareUnpublishRetryBtn').addEventListener('click', function() {
        dismissToast('share');
        handleUnpublish();
      });
    }

    function showShareError(err) {
      if (isSharePolicyRejection(err)) clearSharePolicyCache();
      const el = showToast('share', 'error',
        '<span>Share failed: ' + escapeHtml(shareErrorMessage(err)) + '</span>' +
        '<div class="toast-actions">' +
          '<button class="toast-btn toast-btn-filled" id="shareRetryBtn">Retry</button>' +
          '<button class="toast-btn toast-btn-ghost" data-dismiss-toast="share">Dismiss</button>' +
        '</div>');
      el.querySelector('#shareRetryBtn').addEventListener('click', function() {
        dismissToast('share');
        document.getElementById('shareBtn').click();
      });
    }

    async function beginShareToTarget(target) {
      selectTarget(target);
      // Open popup synchronously BEFORE any await — Safari blocks popups
      // after async gaps. If we end up showing the org modal instead, close it.
      let popupSession = null;
      if (proxyAuth) {
        try { popupSession = openShareReceiver(shareURL); }
        catch (err) { showShareError(err); return; }
      }

      // If user has orgs, show org share modal (handles consent inline)
      if (authUserName) {
        shareBtnEl.disabled = true;
        const results = await Promise.all([fetchOrgs(), fetchSharePolicy(popupSession)]);
        const orgs = results[0];
        const sharePolicy = results[1];
        shareBtnEl.disabled = false;
        if (orgs.length > 0) {
          if (popupSession) popupSession.close();
          showOrgShareModal(orgs, sharePolicy);
          return;
        }
      }

      // No orgs — existing consent gate
      if (needsShareConsent) {
        if (popupSession) popupSession.close();
        showConsentModal();
        return;
      }

      performShare('', '', null, popupSession);
    }

    function showDestinationModal() {
      closeShareModal();
      const overlay = document.createElement('div');
      overlay.className = 'share-overlay';
      overlay.setAttribute('role', 'dialog');
      overlay.setAttribute('aria-modal', 'true');
      overlay.setAttribute('aria-labelledby', 'destinationTitle');
      const rows = shareTargets.map(function(target, index) {
        const external = target.url === 'https://crit.md';
        return '<button class="sd-target-option" data-target-index="' + index + '">' +
          '<span><strong>' + escapeHtml(target.name || target.url) + '</strong><small>' +
          escapeHtml(external ? 'External to your organization · Anyone with the link may view' : target.url) +
          '</small></span><span aria-hidden="true">→</span></button>';
      }).join('');
      overlay.innerHTML = '<div class="share-dialog sd-org-dialog"><div class="sd-org-header">' +
        '<h3 id="destinationTitle" class="sd-org-title">Share review</h3>' +
        '<button class="sd-org-close" aria-label="Close">×</button></div>' +
        '<div class="sd-org-body"><div><label class="sd-org-label">Destination</label>' +
        '<div class="sd-target-list">' + rows + '</div></div></div></div>';
      document.body.appendChild(overlay);
      shareModalEl = overlay;
      overlay.addEventListener('click', function(e) { if (e.target === overlay) closeShareModal(); });
      overlay.addEventListener('keydown', function(e) { if (e.key === 'Escape') closeShareModal(); });
      overlay.querySelector('.sd-org-close').addEventListener('click', closeShareModal);
      overlay.querySelectorAll('.sd-target-option').forEach(function(row) {
        row.addEventListener('click', function() {
          const target = shareTargets[Number(row.getAttribute('data-target-index'))];
          closeShareModal();
          beginShareToTarget(target);
        });
      });
      const first = overlay.querySelector('.sd-target-option');
      if (first) first.focus();
    }

    async function onShareBtnClick() {
      // If already shared, toggle modal. Its saved base URL remains authoritative.
      if (hostedURL) {
        if (shareModalEl) closeShareModal(); else showShareModal();
        return;
      }
      if (shareTargets.length > 1) {
        showDestinationModal();
        return;
      }
      if (!selectedTarget) {
        showShareError(new Error('sharing is disabled in global configuration'));
        return;
      }
      beginShareToTarget(selectedTarget);
    }

    // Announce copy action to screen readers via live region
    function announceCopy() {
      const el = document.getElementById('copyStatus');
      if (el) { el.textContent = ''; el.textContent = 'Copied to clipboard'; }
    }

    // Wire the click handler internally onto the provided #shareBtn element.
    if (shareBtnEl) {
      shareBtnEl.addEventListener('click', onShareBtnClick);
    }

    return {
      // reveal() shows the share button iff (shareURL && canShare), and sets
      // the button to the 'shared' state if there's already a hosted URL.
      reveal: function reveal() {
        if ((hostedURL || shareTargets.length > 0) && options.canShare) {
          const btn = shareBtnEl || document.getElementById('shareBtn');
          if (btn) btn.style.display = '';
          if (hostedURL) setShareButtonState('shared');
        }
      },
      setButtonState: setShareButtonState,
      openModal: showShareModal,
      closeModal: closeShareModal,
    };
  }

  var api = { create: create };
  if (typeof window !== 'undefined') {
    window.crit = window.crit || {};
    window.crit.share = api;
  }
  if (typeof module === 'object' && module.exports) {
    module.exports = api;
  }
})();
