// kpot WebUI — vanilla ES2020, hash-routed SPA.
//
// Views: #/login | #/locked | #/search | #/note/<encoded-name>
//
// Critical iOS Safari constraint: navigator.clipboard.writeText must run
// inside a synchronous user-gesture handler. We pre-fetch secret values
// when a note is opened and cache them in a closure so the copy click
// handler stays sync.

(() => {
  'use strict';

  const $ = (id) => document.getElementById(id);
  const view = $('view');
  const title = $('title');
  const backBtn = $('back-btn');
  const logoutBtn = $('logout-btn');

  // 30s in-page expiry for cached secret values.
  const SECRET_TTL_MS = 30000;

  // ===== util =====

  function escHtml(s) {
    return String(s).replace(/[&<>"']/g, (c) => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
    }[c]));
  }

  function toast(msg, isError) {
    const t = $('toast');
    t.textContent = msg;
    t.style.background = isError ? 'var(--danger)' : '';
    t.style.color = isError ? '#fff' : '';
    t.hidden = false;
    // re-trigger animation
    t.style.animation = 'none';
    void t.offsetWidth;
    t.style.animation = '';
    setTimeout(() => { t.hidden = true; }, 2000);
  }

  async function api(path, opts = {}) {
    const res = await fetch(path, {
      credentials: 'same-origin',
      headers: { 'Accept': 'application/json' },
      ...opts,
    });
    if (res.status === 401) {
      let body = {};
      try { body = await res.json(); } catch (_) {}
      // session/locked → bounce to appropriate view
      if (body.action === 'reauth' || body.action === 'login') {
        location.hash = '#/' + (body.action === 'reauth' ? 'locked' : 'login');
        throw new Error('auth-required');
      }
    }
    if (res.status === 429) {
      throw new Error('Too many attempts — wait a moment.');
    }
    if (!res.ok) {
      let msg = 'request failed';
      try { const b = await res.json(); if (b.error) msg = b.error; } catch (_) {}
      throw new Error(msg);
    }
    return res;
  }

  // ===== router =====

  function route() {
    const h = location.hash || '#/search';
    backBtn.hidden = true;
    logoutBtn.hidden = false;
    if (h === '#/login') { logoutBtn.hidden = true; return renderLogin(); }
    if (h === '#/locked') { logoutBtn.hidden = true; return renderLocked(); }
    if (h.startsWith('#/note/')) {
      backBtn.hidden = false;
      return renderDetail(decodeURIComponent(h.slice('#/note/'.length)));
    }
    return renderSearch();
  }

  backBtn.addEventListener('click', () => { location.hash = '#/search'; });
  logoutBtn.addEventListener('click', async () => {
    try { await fetch('/api/logout', { method: 'POST', credentials: 'same-origin' }); } catch (_) {}
    location.hash = '#/login';
  });

  // ===== login =====

  function renderLogin() {
    title.textContent = 'kpot — login';
    view.innerHTML = `
      <div class="auth-panel">
        <h2>Unlock vault</h2>
        <p>SSH トンネル経由で kpot にアクセスしています。マスターパスフレーズを入力してください。</p>
        <form id="login-form">
          <input type="password" name="passphrase" autocomplete="off"
                 autocapitalize="off" spellcheck="false"
                 placeholder="passphrase" required />
          <button type="submit" class="btn primary">Unlock</button>
          <div class="auth-error" id="login-err"></div>
        </form>
      </div>`;
    $('login-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const pw = e.target.passphrase.value;
      const err = $('login-err');
      err.textContent = '';
      try {
        const r = await fetch('/api/login', {
          method: 'POST',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ passphrase: pw }),
        });
        if (r.ok) {
          location.hash = '#/search';
        } else if (r.status === 429) {
          err.textContent = 'Too many attempts — wait a moment.';
        } else {
          err.textContent = 'Wrong passphrase.';
        }
      } catch (e2) {
        err.textContent = 'Network error: ' + e2.message;
      }
    });
  }

  function renderLocked() {
    title.textContent = 'kpot — locked';
    view.innerHTML = `
      <div class="auth-panel">
        <h2>Session locked</h2>
        <p>アイドルタイムアウトで自動ロックしました。再度パスフレーズを入力してください。</p>
        <form id="login-form">
          <input type="password" name="passphrase" autocomplete="off"
                 autocapitalize="off" spellcheck="false"
                 placeholder="passphrase" required />
          <button type="submit" class="btn primary">Unlock</button>
          <div class="auth-error" id="login-err"></div>
        </form>
      </div>`;
    // Same handler as login.
    $('login-form').addEventListener('submit', async (e) => {
      e.preventDefault();
      const err = $('login-err');
      err.textContent = '';
      try {
        const r = await fetch('/api/login', {
          method: 'POST',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ passphrase: e.target.passphrase.value }),
        });
        if (r.ok) location.hash = '#/search';
        else if (r.status === 429) err.textContent = 'Too many attempts — wait a moment.';
        else err.textContent = 'Wrong passphrase.';
      } catch (e2) { err.textContent = 'Network error: ' + e2.message; }
    });
  }

  // ===== search =====

  let searchAbort = null;
  let searchTimer = null;

  function renderSearch() {
    title.textContent = 'kpot';
    view.innerHTML = `
      <input class="search-box" id="q" type="search"
             autocomplete="off" autocapitalize="off" spellcheck="false"
             placeholder="検索 (note 名 / 本文)" />
      <ul class="results" id="results"></ul>
      <div class="empty" id="empty">クエリを入力するとマッチが表示されます。</div>`;
    const q = $('q');
    q.focus();
    q.addEventListener('input', () => {
      clearTimeout(searchTimer);
      searchTimer = setTimeout(() => doSearch(q.value.trim()), 200);
    });
  }

  async function doSearch(query) {
    const list = $('results');
    const empty = $('empty');
    if (!query) {
      list.innerHTML = '';
      empty.hidden = false;
      empty.textContent = 'クエリを入力するとマッチが表示されます。';
      return;
    }
    if (searchAbort) searchAbort.abort();
    searchAbort = new AbortController();
    try {
      const res = await api('/api/notes?q=' + encodeURIComponent(query),
        { signal: searchAbort.signal });
      const data = await res.json();
      const matches = data.matches || [];
      if (matches.length === 0) {
        list.innerHTML = '';
        empty.hidden = false;
        empty.textContent = '"' + query + '" にマッチするノートはありません。';
        return;
      }
      empty.hidden = true;
      list.innerHTML = matches.map((m) => {
        const tags = [];
        if (m.name_match) tags.push('<span class="result-tag">name</span>');
        if (m.body_match) tags.push('<span class="result-tag">body</span>');
        return `<li>
          <a class="result" href="#/note/${encodeURIComponent(m.name)}">
            <div class="result-name">${escHtml(m.name)}<span class="result-tags">${tags.join('')}</span></div>
            ${m.snippet ? `<div class="result-snippet">${escHtml(m.snippet)}</div>` : ''}
          </a></li>`;
      }).join('');
    } catch (e) {
      if (e.name === 'AbortError') return;
      if (e.message === 'auth-required') return;
      empty.hidden = false;
      empty.textContent = 'Error: ' + e.message;
    }
  }

  // ===== detail =====

  // valueCache holds pre-fetched secret values keyed by field key,
  // wiped on view exit or after SECRET_TTL_MS. Closure-scoped per render.
  function renderDetail(name) {
    title.textContent = name;
    view.innerHTML = '<p class="empty">Loading…</p>';

    const valueCache = new Map();
    const expiryTimers = new Map();

    async function load() {
      let data;
      try {
        const r = await api('/api/notes/' + encodeURIComponent(name));
        data = await r.json();
      } catch (e) {
        if (e.message !== 'auth-required') {
          view.innerHTML = `<p class="empty">Error: ${escHtml(e.message)}</p>`;
        }
        return;
      }
      paint(data);

      // Pre-fetch every field value (secret and non-secret) so click
      // handlers stay synchronous — required by iOS Safari clipboard API.
      for (const f of data.fields) {
        try {
          const r = await api('/api/notes/' + encodeURIComponent(name) +
            '/field/' + encodeURIComponent(f.key));
          const j = await r.json();
          valueCache.set(f.key, j.value);
          scheduleExpiry(f.key);
          // Update non-secret display in place if cached.
          if (!f.is_secret) {
            const el = document.querySelector(`[data-field-display="${cssesc(f.key)}"]`);
            if (el) el.textContent = j.value;
          }
        } catch (e) {
          if (e.message === 'auth-required') return;
        }
      }
    }

    function scheduleExpiry(key) {
      if (expiryTimers.has(key)) clearTimeout(expiryTimers.get(key));
      expiryTimers.set(key, setTimeout(() => {
        valueCache.delete(key);
        expiryTimers.delete(key);
        const btn = document.querySelector(`[data-copy-key="${cssesc(key)}"]`);
        if (btn) btn.textContent = 'Re-fetch';
      }, SECRET_TTL_MS));
    }

    function paint(data) {
      const fieldsHtml = data.fields.map((f) => {
        const val = f.is_secret
          ? '<span class="field-value secret">••••••••</span>'
          : `<span class="field-value" data-field-display="${cssesc(f.key)}">…</span>`;
        const showBtn = f.is_secret
          ? `<button class="btn" data-show-key="${cssesc(f.key)}">Show</button>` : '';
        return `<div class="field-row">
          <div class="field-key">${escHtml(f.key)}</div>
          ${val}
          <div class="field-actions">
            ${showBtn}
            <button class="btn primary" data-copy-key="${cssesc(f.key)}">Copy</button>
          </div>
        </div>`;
      }).join('');

      const urlField = data.fields.find((f) => f.key === 'url');
      const urlBlock = urlField
        ? `<div class="field-row url-row">
             <div class="field-key">url</div>
             <a id="url-link" href="#" target="_blank" rel="noopener noreferrer">open in browser</a>
           </div>`
        : '';

      view.innerHTML = `<div class="detail">
        <h2>${escHtml(data.name)}</h2>
        ${urlBlock}
        ${fieldsHtml || '<p class="empty">(no parseable fields in this note)</p>'}
        ${data.body_redacted
          ? `<div class="body-block">${escHtml(data.body_redacted)}</div>` : ''}
      </div>`;

      // Wire URL link once value cache lands.
      if (urlField) {
        const wire = setInterval(() => {
          const v = valueCache.get('url');
          if (v) {
            $('url-link').href = v;
            clearInterval(wire);
          }
        }, 100);
        setTimeout(() => clearInterval(wire), 5000);
      }

      // Wire copy buttons (synchronous handlers — iOS clipboard requirement).
      view.querySelectorAll('[data-copy-key]').forEach((btn) => {
        btn.addEventListener('click', () => {
          const key = btn.dataset.copyKey;
          const v = valueCache.get(key);
          if (v == null) {
            // Cache expired; re-fetch and prompt user to tap again.
            api('/api/notes/' + encodeURIComponent(name) +
              '/field/' + encodeURIComponent(key))
              .then((r) => r.json())
              .then((j) => {
                valueCache.set(key, j.value);
                scheduleExpiry(key);
                btn.textContent = 'Copy';
                toast('Cache refreshed — tap Copy again.');
              })
              .catch((e) => {
                if (e.message !== 'auth-required') {
                  toast('Error: ' + e.message, true);
                }
              });
            return;
          }
          // Synchronous gesture call — REQUIRED for iOS Safari.
          try {
            navigator.clipboard.writeText(v).then(
              () => toast('Copied "' + key + '" to clipboard.'),
              (e) => toast('Copy failed: ' + e.message, true),
            );
          } catch (e) {
            toast('Copy failed: ' + e.message, true);
          }
        });
      });

      // Wire show buttons (toggle reveal of secret values).
      view.querySelectorAll('[data-show-key]').forEach((btn) => {
        btn.addEventListener('click', () => {
          const key = btn.dataset.showKey;
          const v = valueCache.get(key);
          if (v == null) { toast('Value not yet loaded.'); return; }
          const row = btn.closest('.field-row');
          const valEl = row.querySelector('.field-value');
          if (valEl.dataset.revealed === '1') {
            valEl.textContent = '••••••••';
            valEl.classList.add('secret');
            valEl.dataset.revealed = '0';
            btn.textContent = 'Show';
          } else {
            valEl.textContent = v;
            valEl.classList.remove('secret');
            valEl.dataset.revealed = '1';
            btn.textContent = 'Hide';
            // Auto-revert after 5s.
            setTimeout(() => {
              if (valEl.dataset.revealed === '1') {
                valEl.textContent = '••••••••';
                valEl.classList.add('secret');
                valEl.dataset.revealed = '0';
                btn.textContent = 'Show';
              }
            }, 5000);
          }
        });
      });
    }

    load();

    // Cleanup on next route.
    const cleanup = () => {
      for (const t of expiryTimers.values()) clearTimeout(t);
      valueCache.clear();
      window.removeEventListener('hashchange', cleanup);
    };
    window.addEventListener('hashchange', cleanup);
  }

  // CSS attribute selector escape (limited — keys are [a-z0-9_.-]).
  function cssesc(s) { return String(s).replace(/[^a-zA-Z0-9_-]/g, '\\$&'); }

  // ===== boot =====

  window.addEventListener('hashchange', route);

  // Probe session state to pick the right initial view.
  fetch('/api/status', { credentials: 'same-origin' })
    .then((r) => r.json())
    .then((s) => {
      if (s.state === 'active') {
        if (!location.hash) location.hash = '#/search'; else route();
      } else if (s.state === 'locked') {
        location.hash = '#/locked';
      } else {
        location.hash = '#/login';
      }
    })
    .catch(() => { location.hash = '#/login'; });
})();
