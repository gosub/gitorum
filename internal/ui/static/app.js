'use strict';

/* =============================================================================
   Gitorum SPA — vanilla JS, hash-based routing, no build step
   ============================================================================= */

// ── State ────────────────────────────────────────────────────────────────────
let STATUS = {};

// ── Bootstrap ────────────────────────────────────────────────────────────────
window.addEventListener('DOMContentLoaded', async () => {
  // All interactive elements declare data-action instead of inline handlers,
  // so no repo-controlled string is ever evaluated as JavaScript.
  document.addEventListener('click', e => {
    const el = e.target.closest('[data-action]');
    if (!el || el.tagName === 'FORM') return;
    const fn = ACTIONS[el.dataset.action];
    if (fn) { e.preventDefault(); fn(el.dataset, el); }
  });
  document.addEventListener('submit', e => {
    const fn = ACTIONS[e.target.dataset.action];
    if (fn) { e.preventDefault(); fn(e.target.dataset, e.target); }
  });
  await refreshStatus();
  window.addEventListener('hashchange', route);
  route();
});

// ── API ──────────────────────────────────────────────────────────────────────
async function apiFetch(path, opts = {}) {
  const res = await fetch('/api' + path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  if (!res.ok) {
    let msg;
    try { msg = (await res.json()).error; } catch (_) { msg = await res.text(); }
    throw new Error(msg || res.statusText);
  }
  return res.json();
}

// ── Status & sidebar ─────────────────────────────────────────────────────────
async function refreshStatus() {
  STATUS = await apiFetch('/status').catch(() => ({}));

  $('forum-name').textContent = STATUS.forum_name || 'Gitorum';
  $('identity').textContent   = STATUS.username ? `@${STATUS.username}` : '(anonymous)';
  $('admin-panel').hidden      = !STATUS.is_admin;
  $('trust-warning').hidden    = !STATUS.admin_key_mismatch;

  // Fetch pending join requests and update button badge if admin.
  if (STATUS.is_admin) {
    const reqs = await apiFetch('/admin/requests').catch(() => ({ requests: [] }));
    const count = (reqs.requests || []).length;
    const reqBtn = $('admin-requests-btn');
    if (reqBtn) {
      reqBtn.textContent = count > 0 ? `Join Requests (${count})` : 'Join Requests';
      reqBtn.className   = count > 0 ? 'btn btn-sm btn-primary' : 'btn btn-sm';
    }
  }

  const dot   = $('sync-dot');
  const label = $('sync-label');
  const lastSyncEl = $('last-sync');
  if (STATUS.synced) {
    dot.className    = 'dot green';
    label.textContent = 'Synced';
  } else {
    dot.className    = 'dot red';
    label.textContent = 'Unsynced';
  }
  if (lastSyncEl) {
    lastSyncEl.textContent = STATUS.last_sync_at
      ? 'Last sync: ' + relTime(STATUS.last_sync_at)
      : '';
  }

  const cats = await apiFetch('/categories').catch(() => ({ categories: [] }));
  const ul = $('cat-list');
  ul.innerHTML = '';
  (cats.categories || []).forEach(c => {
    const li = document.createElement('li');
    li.innerHTML = `<a href="#/cat/${u(c.slug)}">${esc(c.name)}</a>`;
    ul.appendChild(li);
  });
}

async function triggerSync() {
  const btn = $('sync-btn');
  btn.disabled = true;
  try {
    await apiFetch('/sync', { method: 'POST' });
    await refreshStatus();
    flash('green');
  } catch (e) {
    flash('red');
    alert('Sync failed: ' + e.message);
  } finally {
    btn.disabled = false;
  }
}

function flash(color) {
  const dot = $('sync-dot');
  dot.className = `dot ${color} flash`;
  setTimeout(() => { dot.className = `dot ${color}`; }, 800);
}

// ── Router ───────────────────────────────────────────────────────────────────
function route() {
  if (!STATUS.initialized) return viewSetup();

  const hash  = location.hash.replace(/^#\/?/, '');
  const parts = hash ? hash.split('/') : [];

  if (parts.length === 0)                                          return viewCategories();
  if (parts[0] === 'cat' && parts.length === 2)                   return viewThreadList(parts[1]);
  if (parts[0] === 'cat' && parts.length === 4
      && parts[2] === 'thread')                                    return viewThread(parts[1], parts[3]);
  if (parts[0] === 'new-thread' && parts.length === 2)            return viewNewThread(parts[1]);
  viewCategories();
}

// ── Views ────────────────────────────────────────────────────────────────────
function viewSetup() {
  render(`
    <div class="setup-wizard">
      <div class="view-header"><h1>Welcome to Gitorum</h1></div>
      <p class="setup-intro">Your forum is not set up yet. Fill in the details below to get started.</p>
      <form class="setup-form" data-action="submitSetup">
        <label>Your username
          <input type="text" id="setup-username" required
            value="${esc(STATUS.username || '')}"
            placeholder="alice"
            ${STATUS.username ? 'readonly' : ''}>
        </label>
        <label>Forum name
          <input type="text" id="setup-forum-name" required placeholder="My Forum">
        </label>
        <label>Remote git URL <small style="font-weight:400">(optional — for syncing with others)</small>
          <input type="text" id="setup-remote" placeholder="https://github.com/you/forum.git">
        </label>
        <div class="form-actions">
          <button type="submit" class="btn btn-primary">Initialize Forum</button>
        </div>
      </form>
    </div>`);
}

async function submitSetup(_, form) {
  const username  = $('setup-username').value.trim();
  const forumName = $('setup-forum-name').value.trim();
  const remoteURL = $('setup-remote').value.trim();
  const btn = form.querySelector('[type=submit]');

  if (remoteURL && !/^(https?:\/\/|git@|ssh:\/\/)/.test(remoteURL)) {
    alert('Remote URL must start with https://, http://, git@, or ssh://');
    return;
  }

  if (btn) btn.disabled = true;
  try {
    await apiFetch('/setup', {
      method: 'POST',
      body: JSON.stringify({ username, forum_name: forumName, remote_url: remoteURL }),
    });
    await refreshStatus();
    location.hash = '';
    route();
  } catch (err) {
    alert('Setup failed: ' + err.message);
    if (btn) btn.disabled = false;
  }
}

async function viewCategories() {
  const data = await apiFetch('/categories').catch(e => {
    render(`<p class="error-msg">Could not load categories: ${esc(e.message)}</p>`);
    return null;
  });
  if (!data) return;

  const cats = data.categories || [];
  let h = `<div class="view-header"><h1>Categories</h1></div>`;
  if (!cats.length) {
    h += STATUS.is_admin
      ? '<p class="empty">No categories yet. Use <strong>Admin › New Category</strong> in the sidebar to create one.</p>'
      : '<p class="empty">No categories yet.</p>';
  } else {
    h += '<div class="card-list">';
    cats.forEach(c => {
      h += `<div class="card">
        <h2><a href="#/cat/${u(c.slug)}">${esc(c.name)}</a></h2>
        <p>${esc(c.description)}</p>
        <small>${c.thread_count} thread${c.thread_count === 1 ? '' : 's'}</small>
      </div>`;
    });
    h += '</div>';
  }
  render(h);
}

async function viewThreadList(catSlug) {
  const data = await apiFetch(`/categories/${u(catSlug)}/threads`).catch(e => {
    render(`<p class="error-msg">Could not load threads: ${esc(e.message)}</p>`);
    return null;
  });
  if (!data) return;

  const threads = data.threads || [];
  const catName = data.category_name || catSlug;

  let h = `<nav class="breadcrumb"><a href="#/">Home</a> › ${esc(catName)}</nav>`;
  h += `<div class="view-header">
    <h1>${esc(catName)}</h1>
    <a class="btn btn-primary" href="#/new-thread/${u(catSlug)}">+ New Thread</a>
  </div>`;

  if (!threads.length) {
    h += '<p class="empty">No threads yet. Be the first to post!</p>';
  } else {
    h += '<div class="card-list">';
    threads.forEach(t => {
      h += `<div class="card${t.hidden ? ' card-deleted' : ''}">
        <h2><a href="#/cat/${u(catSlug)}/thread/${u(t.slug)}">${esc(t.title)}</a></h2>
        <small>
          ${t.hidden ? '' : `by <strong>${esc(t.author)}</strong> ·`}
          ${t.reply_count} repl${t.reply_count === 1 ? 'y' : 'ies'} ·
          ${relTime(t.last_reply_at)}
        </small>
      </div>`;
    });
    h += '</div>';
  }
  render(h);
}

async function viewThread(catSlug, threadSlug) {
  const data = await apiFetch(`/threads/${u(catSlug)}/${u(threadSlug)}`).catch(e => {
    render(`<p class="error-msg">Could not load thread: ${esc(e.message)}</p>`);
    return null;
  });
  if (!data) return;

  const posts = data.posts || [];
  let h = `<nav class="breadcrumb">
    <a href="#/">Home</a> ›
    <a href="#/cat/${u(catSlug)}">${esc(catSlug)}</a> ›
    ${esc(threadSlug)}
  </nav>`;

  posts.forEach((p, i) => {
    if (p.tombstoned) {
      h += `<article class="post post-deleted${i === 0 ? ' post-root' : ''}">
        <header class="post-meta">
          <span class="author">[deleted]</span>
        </header>
        <div class="post-body">${p.body_html}</div>
      </article>`;
      return;
    }
    const deleteBtn = STATUS.is_admin
      ? `<button class="btn btn-danger btn-sm" data-action="showHide" data-cat="${esc(catSlug)}" data-thread="${esc(threadSlug)}" data-filename="${esc(p.filename)}">Hide</button>`
      : '';
    h += `<article class="post${i === 0 ? ' post-root' : ''}">
      <header class="post-meta">
        <span class="author">${esc(p.author)}</span>
        ${sigBadge(p)}
        <time class="ts" title="${esc(p.timestamp)}">${relTime(p.timestamp)}</time>
        ${deleteBtn}
      </header>
      <div class="post-body">${p.body_html}</div>
    </article>`;
  });

  h += STATUS.username
    ? `<section class="reply-form">
        <h3>Post a Reply</h3>
        <textarea id="reply-body" rows="6" placeholder="Your reply (Markdown supported)…"></textarea>
        <div class="form-actions">
          <button class="btn btn-primary" data-action="submitReply" data-cat="${esc(catSlug)}" data-thread="${esc(threadSlug)}">Submit Reply</button>
        </div>
      </section>`
    : `<section class="reply-form">
        <p class="empty">No identity configured. Run <code>gitorum keygen</code> to enable posting.</p>
      </section>`;

  render(h);
}

function viewNewThread(catSlug) {
  render(`
    <nav class="breadcrumb">
      <a href="#/">Home</a> ›
      <a href="#/cat/${u(catSlug)}">${esc(catSlug)}</a> ›
      New Thread
    </nav>
    <h1 style="margin-bottom:1.25rem">New Thread</h1>
    <form class="new-thread-form" data-action="submitNewThread" data-cat="${esc(catSlug)}">
      <label>Title
        <input type="text" id="nt-title" required placeholder="Thread title">
      </label>
      <label>Thread slug <small style="font-weight:400">(lowercase letters, numbers, hyphens)</small>
        <input type="text" id="nt-slug" pattern="[a-z0-9-]+" required placeholder="my-first-thread">
      </label>
      <label>Body <small style="font-weight:400">(Markdown supported)</small>
        <textarea id="nt-body" rows="10" placeholder="Write your post here…"></textarea>
      </label>
      <div class="form-actions">
        <button type="submit" class="btn btn-primary">Create Thread</button>
        <a class="btn" href="#/cat/${u(catSlug)}">Cancel</a>
      </div>
    </form>`);
}

// ── Actions ──────────────────────────────────────────────────────────────────
async function submitReply({ cat: catSlug, thread: threadSlug }) {
  const bodyEl = $('reply-body');
  const body   = bodyEl.value.trim();
  if (!body) return;

  const btn = document.querySelector('.reply-form .btn-primary');
  if (btn) btn.disabled = true;

  try {
    await apiFetch(`/threads/${u(catSlug)}/${u(threadSlug)}/reply`, {
      method: 'POST',
      body:   JSON.stringify({ body }),
    });
    bodyEl.value = '';
    await viewThread(catSlug, threadSlug);
    window.scrollTo(0, document.body.scrollHeight);
  } catch (e) {
    alert('Submit failed: ' + e.message);
    if (btn) btn.disabled = false;
  }
}

async function submitNewThread({ cat: catSlug }) {
  const title   = $('nt-title').value.trim();
  const slug    = $('nt-slug').value.trim();
  const bodyRaw = $('nt-body').value.trim();
  const body    = title ? `# ${title}\n\n${bodyRaw}` : bodyRaw;
  if (!slug || !body) return;

  try {
    await apiFetch('/threads', {
      method: 'POST',
      body:   JSON.stringify({ category: catSlug, slug, body }),
    });
    location.hash = `#/cat/${u(catSlug)}/thread/${u(slug)}`;
  } catch (e) {
    alert('Error: ' + e.message);
  }
}

function showHide({ cat, thread, filename }) {
  openModal(`
    <h2>Hide post</h2>
    <p>Hiding adds an admin-signed tombstone: everyone sees <em>[deleted]</em>
      instead of the post. The file stays in the repository and in git history,
      so anyone with a copy can still read it.</p>
    <label class="checkbox"><input type="checkbox" id="hide-purge">
      Also remove the file from the current tree (purge). It stays in git history.</label>
    <div class="form-actions">
      <button class="btn btn-danger" data-action="adminDelete" data-cat="${esc(cat)}"
        data-thread="${esc(thread)}" data-filename="${esc(filename)}">Hide</button>
      <button class="btn" data-action="closeModal">Cancel</button>
    </div>`);
}

async function adminDelete({ cat: catSlug, thread: threadSlug, filename }) {
  const purge = $('hide-purge')?.checked || false;
  try {
    await apiFetch('/admin/delete', {
      method: 'POST',
      body:   JSON.stringify({ category: catSlug, thread: threadSlug, filename, purge }),
    });
    closeModal();
    await viewThread(catSlug, threadSlug);
  } catch (e) {
    alert('Hide failed: ' + e.message);
  }
}

function showAdminAddKey() {
  openModal(`
    <h2>Add User Key</h2>
    <label>Username
      <input type="text" id="ak-username" placeholder="alice">
    </label>
    <label>Ed25519 public key (base64)
      <input type="text" id="ak-pubkey" placeholder="AAAA…">
    </label>
    <label class="checkbox"><input type="checkbox" id="ak-rotate">
      Key rotation: add this key to an existing user</label>
    <div class="form-actions">
      <button class="btn btn-primary" data-action="submitAddKey">Add Key</button>
      <button class="btn" data-action="closeModal">Cancel</button>
    </div>`);
}

function showAdminCreateCategory() {
  openModal(`
    <h2>New Category</h2>
    <label>Slug <small style="font-weight:400">(lowercase letters, numbers, hyphens)</small>
      <input type="text" id="nc-slug" placeholder="my-category">
    </label>
    <label>Name
      <input type="text" id="nc-name" placeholder="My Category">
    </label>
    <label>Description <small style="font-weight:400">(optional)</small>
      <input type="text" id="nc-desc" placeholder="What this category is about…">
    </label>
    <div class="form-actions">
      <button class="btn btn-primary" data-action="submitCreateCategory">Create</button>
      <button class="btn" data-action="closeModal">Cancel</button>
    </div>`);
}

async function submitCreateCategory() {
  const slug        = $('nc-slug').value.trim();
  const name        = $('nc-name').value.trim();
  const description = $('nc-desc').value.trim();
  if (!slug || !name) { alert('Slug and name are required.'); return; }
  try {
    await apiFetch('/categories', {
      method: 'POST',
      body:   JSON.stringify({ slug, name, description }),
    });
    closeModal();
    await refreshStatus();
    location.hash = `#/cat/${u(slug)}`;
    route();
  } catch (e) {
    alert('Error: ' + e.message);
  }
}

async function showJoinRequests() {
  let data;
  try {
    data = await apiFetch('/admin/requests');
  } catch (e) {
    alert('Error: ' + e.message);
    return;
  }

  const requests = data.requests || [];
  let h = '<h2>Join Requests</h2>';

  if (!requests.length) {
    h += '<p class="empty" style="margin:.75rem 0">No pending requests.</p>';
  } else {
    requests.forEach(req => {
      h += `<div class="join-req-item">
        <strong>@${esc(req.username)}</strong>
        <div class="join-req-key">${esc(req.pubkey)}</div>
        <div class="form-actions" style="margin-top:.4rem">
          <button class="btn btn-primary btn-sm" data-action="approveJoinRequest" data-username="${esc(req.username)}">Approve</button>
          <button class="btn btn-danger btn-sm" data-action="rejectJoinRequest" data-username="${esc(req.username)}">Reject</button>
        </div>
      </div>`;
    });
  }
  h += '<div class="form-actions" style="margin-top:.75rem"><button class="btn" data-action="closeModal">Close</button></div>';
  openModal(h);
}

async function approveJoinRequest({ username }) {
  try {
    await apiFetch('/admin/approve', { method: 'POST', body: JSON.stringify({ username }) });
    await refreshStatus();
    await showJoinRequests();
  } catch (e) {
    alert('Error: ' + e.message);
  }
}

async function rejectJoinRequest({ username }) {
  if (!confirm(`Reject join request from @${username}?`)) return;
  try {
    await apiFetch('/admin/reject', { method: 'POST', body: JSON.stringify({ username }) });
    await refreshStatus();
    await showJoinRequests();
  } catch (e) {
    alert('Error: ' + e.message);
  }
}

async function submitAddKey() {
  const username = $('ak-username').value.trim();
  const pubkey   = $('ak-pubkey').value.trim();
  const rotate   = $('ak-rotate').checked;
  if (!username || !pubkey) { alert('Both fields are required.'); return; }
  try {
    await apiFetch('/admin/addkey', {
      method: 'POST',
      body:   JSON.stringify({ username, pubkey, rotate }),
    });
    closeModal();
    alert(`Key for @${username} added.`);
  } catch (e) {
    alert('Error: ' + e.message);
  }
}

// ── Action registry ──────────────────────────────────────────────────────────
const ACTIONS = {
  triggerSync, submitSetup, submitReply, submitNewThread, adminDelete, showHide,
  showAdminAddKey, showAdminCreateCategory, showJoinRequests,
  submitAddKey, submitCreateCategory, approveJoinRequest, rejectJoinRequest,
  closeModal: () => closeModal(),
};

// ── Modal ────────────────────────────────────────────────────────────────────
function openModal(html) {
  closeModal();
  const overlay = document.createElement('div');
  overlay.id        = 'modal-overlay';
  overlay.className = 'modal-overlay';
  overlay.innerHTML = `<div class="modal">${html}</div>`;
  overlay.addEventListener('click', e => { if (e.target === overlay) closeModal(); });
  document.body.appendChild(overlay);
}

function closeModal() {
  const m = $('modal-overlay');
  if (m) m.remove();
}

// ── Utilities ────────────────────────────────────────────────────────────────
function $(id) { return document.getElementById(id); }
function render(html) { $('view').innerHTML = html; }

function esc(s) {
  return String(s ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// u encodes a value for use as a URL path segment or hash component.
function u(s) { return encodeURIComponent(String(s ?? '')); }

function sigBadge(post) {
  switch (post.sig_status) {
    case 'valid':   return `<span class="badge badge-ok"   title="Signature verified">✓ signed</span>`;
    case 'untrusted': return `<span class="badge badge-warn" title="${esc(post.sig_error)}">! untrusted key</span>`;
    case 'invalid': return `<span class="badge badge-err"  title="${esc(post.sig_error)}">✗ invalid sig</span>`;
    case 'missing': return `<span class="badge badge-warn" title="${esc(post.sig_error)}">? no key</span>`;
    default:        return '';
  }
}

function relTime(iso) {
  if (!iso) return '';
  const ms = Date.now() - new Date(iso).getTime();
  if (isNaN(ms))         return iso;
  if (ms <      60_000)  return 'just now';
  if (ms <   3_600_000)  return `${Math.floor(ms /      60_000)}m ago`;
  if (ms <  86_400_000)  return `${Math.floor(ms /   3_600_000)}h ago`;
  if (ms < 604_800_000)  return `${Math.floor(ms /  86_400_000)}d ago`;
  return new Date(iso).toLocaleDateString();
}
