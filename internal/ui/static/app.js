'use strict';

/* =============================================================================
   Gitorum SPA — vanilla JS, hash-based routing, no build step
   ============================================================================= */

// ── State ────────────────────────────────────────────────────────────────────
let STATUS = {};

// ── Bootstrap ────────────────────────────────────────────────────────────────
window.addEventListener('DOMContentLoaded', async () => {
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

  const dot   = $('sync-dot');
  const label = $('sync-label');
  if (STATUS.synced) {
    dot.className    = 'dot green';
    label.textContent = 'Synced';
  } else {
    dot.className    = 'dot red';
    label.textContent = 'Unsynced';
  }

  const cats = await apiFetch('/categories').catch(() => ({ categories: [] }));
  const ul = $('cat-list');
  ul.innerHTML = '';
  (cats.categories || []).forEach(c => {
    const li = document.createElement('li');
    li.innerHTML = `<a href="#/cat/${c.slug}">${esc(c.name)}</a>`;
    ul.appendChild(li);
  });
}

async function triggerSync() {
  const btn = $('sync-btn');
  btn.disabled = true;
  try {
    await apiFetch('/sync');
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
      <form class="setup-form" onsubmit="submitSetup(event)">
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

async function submitSetup(e) {
  e.preventDefault();
  const username  = $('setup-username').value.trim();
  const forumName = $('setup-forum-name').value.trim();
  const remoteURL = $('setup-remote').value.trim();
  const btn = e.target.querySelector('[type=submit]');
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
    h += '<p class="empty">No categories yet.</p>';
  } else {
    h += '<div class="card-list">';
    cats.forEach(c => {
      h += `<div class="card">
        <h2><a href="#/cat/${c.slug}">${esc(c.name)}</a></h2>
        <p>${esc(c.description)}</p>
        <small>${c.thread_count} thread${c.thread_count === 1 ? '' : 's'}</small>
      </div>`;
    });
    h += '</div>';
  }
  render(h);
}

async function viewThreadList(catSlug) {
  const data = await apiFetch(`/categories/${catSlug}/threads`).catch(e => {
    render(`<p class="error-msg">Could not load threads: ${esc(e.message)}</p>`);
    return null;
  });
  if (!data) return;

  const threads = data.threads || [];
  const catName = data.category_name || catSlug;

  let h = `<nav class="breadcrumb"><a href="#/">Home</a> › ${esc(catName)}</nav>`;
  h += `<div class="view-header">
    <h1>${esc(catName)}</h1>
    <a class="btn btn-primary" href="#/new-thread/${catSlug}">+ New Thread</a>
  </div>`;

  if (!threads.length) {
    h += '<p class="empty">No threads yet. Be the first to post!</p>';
  } else {
    h += '<div class="card-list">';
    threads.forEach(t => {
      h += `<div class="card">
        <h2><a href="#/cat/${catSlug}/thread/${t.slug}">${esc(t.title)}</a></h2>
        <small>
          by <strong>${esc(t.author)}</strong> ·
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
  const data = await apiFetch(`/threads/${catSlug}/${threadSlug}`).catch(e => {
    render(`<p class="error-msg">Could not load thread: ${esc(e.message)}</p>`);
    return null;
  });
  if (!data) return;

  const posts = data.posts || [];
  let h = `<nav class="breadcrumb">
    <a href="#/">Home</a> ›
    <a href="#/cat/${catSlug}">${esc(catSlug)}</a> ›
    ${esc(threadSlug)}
  </nav>`;

  posts.forEach((p, i) => {
    const deleteBtn = STATUS.is_admin
      ? `<button class="btn btn-danger btn-sm" onclick="adminDelete('${esc(catSlug)}','${esc(threadSlug)}','${esc(p.filename)}')">Delete</button>`
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

  h += `<section class="reply-form">
    <h3>Post a Reply</h3>
    <textarea id="reply-body" rows="6" placeholder="Your reply (Markdown supported)…"></textarea>
    <div class="form-actions">
      <button class="btn btn-primary" onclick="submitReply('${esc(catSlug)}','${esc(threadSlug)}')">Submit Reply</button>
    </div>
  </section>`;

  render(h);
}

function viewNewThread(catSlug) {
  render(`
    <nav class="breadcrumb">
      <a href="#/">Home</a> ›
      <a href="#/cat/${catSlug}">${esc(catSlug)}</a> ›
      New Thread
    </nav>
    <h1 style="margin-bottom:1.25rem">New Thread</h1>
    <form class="new-thread-form" onsubmit="submitNewThread(event,'${esc(catSlug)}')">
      <label>Thread slug <small style="font-weight:400">(lowercase letters, numbers, hyphens)</small>
        <input type="text" id="nt-slug" pattern="[a-z0-9-]+" required placeholder="my-first-thread">
      </label>
      <label>Body <small style="font-weight:400">(Markdown supported)</small>
        <textarea id="nt-body" rows="10" required placeholder="# Thread Title&#10;&#10;Write your post here…"></textarea>
      </label>
      <div class="form-actions">
        <button type="submit" class="btn btn-primary">Create Thread</button>
        <a class="btn" href="#/cat/${esc(catSlug)}">Cancel</a>
      </div>
    </form>`);
}

// ── Actions ──────────────────────────────────────────────────────────────────
async function submitReply(catSlug, threadSlug) {
  const bodyEl = $('reply-body');
  const body   = bodyEl.value.trim();
  if (!body) return;

  const btn = document.querySelector('.reply-form .btn-primary');
  if (btn) btn.disabled = true;

  try {
    await apiFetch(`/threads/${catSlug}/${threadSlug}/reply`, {
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

async function submitNewThread(e, catSlug) {
  e.preventDefault();
  const slug = $('nt-slug').value.trim();
  const body = $('nt-body').value.trim();
  if (!slug || !body) return;

  try {
    await apiFetch('/threads', {
      method: 'POST',
      body:   JSON.stringify({ category: catSlug, slug, body }),
    });
    location.hash = `#/cat/${catSlug}/thread/${slug}`;
  } catch (e) {
    alert('Error: ' + e.message);
  }
}

async function adminDelete(catSlug, threadSlug, filename) {
  if (!confirm(`Delete "${filename}"?\nThis creates a signed tombstone and is permanent.`)) return;
  try {
    await apiFetch('/admin/delete', {
      method: 'POST',
      body:   JSON.stringify({ category: catSlug, thread: threadSlug, filename }),
    });
    await viewThread(catSlug, threadSlug);
  } catch (e) {
    alert('Delete failed: ' + e.message);
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
    <div class="form-actions">
      <button class="btn btn-primary" onclick="submitAddKey()">Add Key</button>
      <button class="btn" onclick="closeModal()">Cancel</button>
    </div>`);
}

async function submitAddKey() {
  const username = $('ak-username').value.trim();
  const pubkey   = $('ak-pubkey').value.trim();
  if (!username || !pubkey) { alert('Both fields are required.'); return; }
  try {
    await apiFetch('/admin/addkey', {
      method: 'POST',
      body:   JSON.stringify({ username, pubkey }),
    });
    closeModal();
    alert(`Key for @${username} added.`);
  } catch (e) {
    alert('Error: ' + e.message);
  }
}

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
    .replace(/"/g, '&quot;');
}

function sigBadge(post) {
  switch (post.sig_status) {
    case 'valid':   return `<span class="badge badge-ok"   title="Signature verified">✓ signed</span>`;
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
