/* ─── BeatportDL UI — App ───────────────────────────────────────────────────── */

const $ = (sel, ctx = document) => ctx.querySelector(sel);
const $$ = (sel, ctx = document) => [...ctx.querySelectorAll(sel)];

// ─── State ───────────────────────────────────────────────────────────────────
const state = {
  jobs: {},          // { [jobID]: job }
  settings: null,
  quality: 'lossless',
  wsReady: false,
  searchType: 'all',
  searchQuery: '',
  searchController: null,
};

// ─── WebSocket ────────────────────────────────────────────────────────────────
let ws = null;
let wsReconnectTimer = null;

function connectWS() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  ws = new WebSocket(`${proto}://${location.host}/api/ws`);

  ws.addEventListener('open', () => {
    state.wsReady = true;
    clearTimeout(wsReconnectTimer);
  });

  ws.addEventListener('message', (e) => {
    try {
      const msg = JSON.parse(e.data);
      handleWSMessage(msg);
    } catch (_) {}
  });

  ws.addEventListener('close', () => {
    state.wsReady = false;
    wsReconnectTimer = setTimeout(connectWS, 2000);
  });

  ws.addEventListener('error', () => ws.close());
}

function handleWSMessage(msg) {
  switch (msg.type) {
    case 'job_update':
      updateJob(msg.payload);
      break;
    case 'track_progress':
      updateTrackProgress(msg.payload);
      break;
    case 'fix_progress':
      appendFixLog(msg.payload.message);
      break;
  }
}

// ─── Navigation ───────────────────────────────────────────────────────────────
function initNav() {
  $$('.nav-item').forEach(btn => {
    btn.addEventListener('click', () => {
      const view = btn.dataset.view;
      $$('.nav-item').forEach(b => b.classList.remove('active'));
      $$('.view').forEach(v => v.classList.remove('active'));
      btn.classList.add('active');
      $(`#view-${view}`).classList.add('active');

    });
  });
}

// ─── Settings ─────────────────────────────────────────────────────────────────
async function loadSettings() {
  try {
    const res = await fetch('/api/settings');
    if (!res.ok) return;
    state.settings = await res.json();
    populateSettingsForm(state.settings);
    updateAuthStatus(!!state.settings.username);
    // Sync quality pill to saved setting
    if (state.settings.quality) {
      state.quality = state.settings.quality;
      $$('.pill', $('#quality-pills')).forEach(p => {
        p.classList.toggle('active', p.dataset.value === state.quality);
      });
    }
  } catch (_) {}
}

function populateSettingsForm(s) {
  const form = $('#settings-form');
  if (!form) return;
  Object.entries(s).forEach(([key, val]) => {
    const el = form.elements[key];
    if (!el) return;
    if (el.type === 'checkbox') el.checked = !!val;
    else el.value = val ?? '';
  });
}

function initSettingsForm() {
  const form = $('#settings-form');
  if (!form) return;

  // Password eye toggle
  const pwInput = form.elements['password'];
  $('#btn-eye')?.addEventListener('click', () => {
    pwInput.type = pwInput.type === 'password' ? 'text' : 'password';
  });

  // Test auth
  $('#btn-test-auth')?.addEventListener('click', async () => {
    const result = $('#auth-result');
    result.textContent = 'Testing…';
    result.className = 'auth-result';

    // Save current form values first
    await saveSettingsFromForm();

    try {
      const res = await fetch('/api/auth/test', { method: 'POST' });
      const data = await res.json();
      if (res.ok) {
        result.textContent = '✓ Authenticated successfully';
        result.className = 'auth-result ok';
        updateAuthStatus(true);
      } else {
        result.textContent = '✗ ' + (data.error || 'Authentication failed');
        result.className = 'auth-result err';
        updateAuthStatus(false);
      }
    } catch (e) {
      result.textContent = '✗ Request failed';
      result.className = 'auth-result err';
    }
  });

  // Save settings
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    await saveSettingsFromForm();
    const fb = $('#save-feedback');
    fb.textContent = 'Saved ✓';
    fb.classList.add('visible');
    setTimeout(() => fb.classList.remove('visible'), 2000);
  });
}

async function saveSettingsFromForm() {
  const form = $('#settings-form');
  const data = {};
  $$('input, select', form).forEach(el => {
    if (!el.name) return;
    if (el.type === 'checkbox') data[el.name] = el.checked;
    else if (el.type === 'number') data[el.name] = el.value ? Number(el.value) : 0;
    else data[el.name] = el.value;
  });

  try {
    const res = await fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    });
    if (res.ok) {
      state.settings = data;
      updateAuthStatus(!!data.username);
    }
  } catch (_) {}
}

function updateAuthStatus(ok) {
  const dot = $('#auth-dot');
  const label = $('#auth-label');
  if (ok) {
    dot.className = 'status-dot ok';
    label.textContent = state.settings?.username || 'Authenticated';
  } else {
    dot.className = 'status-dot err';
    label.textContent = 'Not authenticated';
  }
}

// ─── Search ───────────────────────────────────────────────────────────────────
let searchDebounceTimer = null;

function initSearch() {
  const input = $('#search-input');
  const tabs = $('#search-tabs');

  tabs?.addEventListener('click', (e) => {
    const pill = e.target.closest('.pill');
    if (!pill) return;
    $$('.pill', tabs).forEach(p => p.classList.remove('active'));
    pill.classList.add('active');
    state.searchType = pill.dataset.type;
    if (state.searchQuery.length >= 2) runSearch(state.searchQuery);
  });

  input?.addEventListener('input', () => {
    const q = input.value.trim();
    clearTimeout(searchDebounceTimer);
    if (q.length < 2) {
      state.searchQuery = '';
      renderSearchEmpty('Enter at least 2 characters to search');
      setSearchStatus('');
      return;
    }
    searchDebounceTimer = setTimeout(() => runSearch(q), 350);
  });

  input?.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      clearTimeout(searchDebounceTimer);
      const q = input.value.trim();
      if (q.length >= 2) runSearch(q);
    }
  });
}

async function runSearch(query) {
  state.searchQuery = query;
  if (state.searchController) state.searchController.abort();
  state.searchController = new AbortController();

  setSearchStatus('Searching…', 'loading');
  const resultsEl = $('#search-results');
  resultsEl.innerHTML = '<div class="search-loading">Searching Beatport catalog…</div>';

  try {
    const params = new URLSearchParams({
      q: query,
      type: state.searchType,
      page: '1',
      per_page: '25',
    });
    const res = await fetch(`/api/search?${params}`, { signal: state.searchController.signal });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Search failed');

    renderSearchResults(data);
    const trackCount = data.tracks?.items?.length || 0;
    const artistCount = data.artists?.items?.length || 0;
    const total = trackCount + artistCount;
    if (total === 0) {
      setSearchStatus('No results found', 'empty');
    } else {
      const parts = [];
      if (trackCount) parts.push(`${trackCount} track${trackCount !== 1 ? 's' : ''}`);
      if (artistCount) parts.push(`${artistCount} artist${artistCount !== 1 ? 's' : ''}`);
      setSearchStatus(`Found ${parts.join(', ')}`, 'ok');
    }
  } catch (e) {
    if (e.name === 'AbortError') return;
    renderSearchEmpty(e.message);
    setSearchStatus(e.message, 'error');
  }
}

function setSearchStatus(msg, kind = '') {
  const el = $('#search-status');
  if (!el) return;
  if (!msg) { el.style.display = 'none'; return; }
  el.style.display = '';
  el.textContent = msg;
  el.className = 'search-status' + (kind ? ' ' + kind : '');
}

function renderSearchEmpty(msg) {
  const resultsEl = $('#search-results');
  resultsEl.innerHTML = `<div class="empty-state">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
    <p>${escHtml(msg)}</p>
  </div>`;
}

function renderSearchResults(data) {
  const resultsEl = $('#search-results');
  const sections = [];

  if (data.tracks?.items?.length) {
    sections.push(`
      <div class="search-section">
        <h2 class="search-section-title">Tracks <span class="search-count">${data.tracks.count || data.tracks.items.length}</span></h2>
        <div class="search-list">
          ${data.tracks.items.map(t => searchTrackHTML(t)).join('')}
        </div>
      </div>`);
  }

  if (data.artists?.items?.length) {
    sections.push(`
      <div class="search-section">
        <h2 class="search-section-title">Artists <span class="search-count">${data.artists.count || data.artists.items.length}</span></h2>
        <div class="search-list">
          ${data.artists.items.map(a => searchArtistHTML(a)).join('')}
        </div>
      </div>`);
  }

  if (sections.length === 0) {
    renderSearchEmpty('No results found');
    return;
  }

  resultsEl.innerHTML = sections.join('');
  bindSearchActions(resultsEl);
}

function searchTrackHTML(t) {
  const meta = [t.artists, t.genre, t.bpm ? `${t.bpm} BPM` : '', t.key, t.length].filter(Boolean).join(' · ');
  const img = t.image_uri
    ? `<img class="search-thumb" src="${escHtml(t.image_uri)}" alt="" loading="lazy" />`
    : `<div class="search-thumb placeholder"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="3"/></svg></div>`;

  return `
    <div class="search-item" data-url="${escHtml(t.url)}">
      ${img}
      <div class="search-item-info">
        <div class="search-item-title">${escHtml(t.title)}</div>
        <div class="search-item-meta">${escHtml(meta)}</div>
        ${t.label ? `<div class="search-item-label">${escHtml(t.label)}</div>` : ''}
      </div>
      <div class="search-item-actions">
        <a class="btn-icon" href="${escHtml(t.url)}" target="_blank" rel="noopener" title="Open on Beatport" onclick="event.stopPropagation()">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/><polyline points="15,3 21,3 21,9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
        </a>
        <button class="btn-search-download" title="Download track">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7,10 12,15 17,10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
        </button>
      </div>
    </div>`;
}

function searchArtistHTML(a) {
  const img = a.image_uri
    ? `<img class="search-thumb round" src="${escHtml(a.image_uri)}" alt="" loading="lazy" />`
    : `<div class="search-thumb round placeholder"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2"/><circle cx="12" cy="7" r="4"/></svg></div>`;

  return `
    <div class="search-item" data-url="${escHtml(a.url)}">
      ${img}
      <div class="search-item-info">
        <div class="search-item-title">${escHtml(a.name)}</div>
        <div class="search-item-meta">Artist</div>
      </div>
      <div class="search-item-actions">
        <a class="btn-icon" href="${escHtml(a.url)}" target="_blank" rel="noopener" title="Open on Beatport" onclick="event.stopPropagation()">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/><polyline points="15,3 21,3 21,9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
        </a>
        <button class="btn-search-download" title="Download all tracks">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7,10 12,15 17,10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
        </button>
      </div>
    </div>`;
}

function bindSearchActions(container) {
  $$('.btn-search-download', container).forEach(btn => {
    btn.addEventListener('click', e => {
      e.stopPropagation();
      const url = btn.closest('.search-item')?.dataset.url;
      if (url) downloadFromSearch(url, btn);
    });
  });
}

async function downloadFromSearch(url, btn) {
  const origHTML = btn.innerHTML;
  btn.disabled = true;
  btn.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="animation:spin 0.8s linear infinite"><path d="M21 12a9 9 0 11-18 0 9 9 0 0118 0"/></svg>`;

  try {
    const res = await fetch('/api/download', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, quality: state.quality }),
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Download request failed');

    toast(`Job ${data.job_id} queued`);
    setTimeout(() => {
      $$('.nav-item').forEach(b => b.classList.remove('active'));
      $$('.view').forEach(v => v.classList.remove('active'));
      $('[data-view="queue"]').classList.add('active');
      $('#view-queue').classList.add('active');
    }, 400);
  } catch (e) {
    toast(e.message, 'error');
  } finally {
    btn.disabled = false;
    btn.innerHTML = origHTML;
  }
}

// ─── Download ─────────────────────────────────────────────────────────────────
function initDownload() {
  // Quality pills
  $$('.pill', $('#quality-pills')).forEach(pill => {
    pill.addEventListener('click', () => {
      $$('.pill', $('#quality-pills')).forEach(p => p.classList.remove('active'));
      pill.classList.add('active');
      state.quality = pill.dataset.value;
    });
  });

  // Paste button
  $('#btn-paste')?.addEventListener('click', async () => {
    try {
      const text = await navigator.clipboard.readText();
      $('#url-input').value = text.trim();
      $('#url-input').focus();
    } catch (_) {
      toast('Clipboard access denied — paste manually', 'warn');
    }
  });

  // Download button
  $('#btn-download')?.addEventListener('click', startDownload);
  $('#url-input')?.addEventListener('keydown', e => {
    if (e.key === 'Enter') startDownload();
  });

  // Handle drag-drop URLs
  document.addEventListener('dragover', e => e.preventDefault());
  document.addEventListener('drop', e => {
    e.preventDefault();
    const url = e.dataTransfer.getData('text/plain') || e.dataTransfer.getData('text/uri-list');
    if (url) {
      $('#url-input').value = url.trim();
      if ($('#view-download').classList.contains('active')) {
        startDownload();
      }
    }
  });
}

async function startDownload() {
  const urlInput = $('#url-input');
  const url = urlInput.value.trim();
  if (!url) return;

  const btn = $('#btn-download');
  btn.disabled = true;
  btn.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="animation:spin 0.8s linear infinite"><path d="M21 12a9 9 0 11-18 0 9 9 0 0118 0"/></svg> Starting…`;

  try {
    const res = await fetch('/api/download', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, quality: state.quality }),
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Download request failed');

    urlInput.value = '';
    toast(`Job ${data.job_id} queued`);

    // Switch to queue view after a beat
    setTimeout(() => {
      $$('.nav-item').forEach(b => b.classList.remove('active'));
      $$('.view').forEach(v => v.classList.remove('active'));
      $('[data-view="queue"]').classList.add('active');
      $('#view-queue').classList.add('active');
    }, 400);

  } catch (e) {
    toast(e.message, 'error');
  } finally {
    btn.disabled = false;
    btn.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7,10 12,15 17,10"/><line x1="12" y1="15" x2="12" y2="3"/></svg> Download`;
  }
}

// ─── Job / Queue ──────────────────────────────────────────────────────────────
function updateJob(payload) {
  state.jobs[payload.job_id] = payload;
  renderQueue();
  updateQueueBadge();
  renderRecent();
}

function updateTrackProgress(payload) {
  const job = state.jobs[payload.job_id];
  if (!job) return;
  const track = job.tracks?.find(t => t.id === payload.track_id);
  if (track) {
    track.status = payload.status;
    track._progress = payload.progress;
    track._msg = payload.message;
  }
  // Update the DOM directly for performance
  const row = document.getElementById(`track-${payload.job_id}-${payload.track_id}`);
  if (row) {
    const dot = row.querySelector('.track-dot');
    const prog = row.querySelector('.track-progress');
    if (dot) dot.className = `track-dot ${payload.status}`;
    if (prog) {
      prog.textContent = payload.progress < 100 ? Math.round(payload.progress) + '%' : '✓';
      prog.className = 'track-progress' + (payload.status === 'error' ? ' error' : '');
    }
  }
}

function renderQueue() {
  const list = $('#queue-list');
  const jobs = Object.values(state.jobs).sort((a, b) => b.job_id.localeCompare(a.job_id));

  if (jobs.length === 0) {
    list.innerHTML = `<div class="empty-state">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7,10 12,15 17,10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
      <p>No downloads yet</p>
    </div>`;
    return;
  }

  // Only re-render cards for new/changed jobs
  jobs.forEach(job => {
    const existing = document.getElementById(`job-${job.job_id}`);
    if (!existing) {
      const card = buildJobCard(job);
      list.querySelector('.empty-state')?.remove();
      list.prepend(card);
    } else {
      updateJobCard(existing, job);
    }
  });
}

function buildJobCard(job) {
  const card = document.createElement('div');
  card.className = 'job-card';
  card.id = `job-${job.job_id}`;
  card.innerHTML = jobCardHTML(job);
  // Toggle tracks
  card.querySelector('.job-header').addEventListener('click', e => {
    if (e.target.closest('.btn-icon')) return;
    card.querySelector('.job-tracks').classList.toggle('open');
  });
  // Zip download button
  card.querySelector('.btn-zip-job')?.addEventListener('click', e => {
    e.stopPropagation();
    window.location.href = `/api/jobs/${job.job_id}/zip`;
  });
  // Delete button
  card.querySelector('.btn-delete-job')?.addEventListener('click', async e => {
    e.stopPropagation();
    await fetch(`/api/jobs/${job.job_id}`, { method: 'DELETE' });
    delete state.jobs[job.job_id];
    card.remove();
    updateQueueBadge();
    if (Object.keys(state.jobs).length === 0) renderQueue();
  });
  return card;
}

function updateJobCard(card, job) {
  const icon   = card.querySelector('.job-status-icon');
  const sub    = card.querySelector('.job-sub');
  const fill   = card.querySelector('.job-progress-fill');
  const actions = card.querySelector('.job-actions');
  if (icon) { icon.className = `job-status-icon ${job.status}`; icon.innerHTML = statusIcon(job.status); }
  const urlEl = card.querySelector('.job-url');
  if (urlEl && job.name) urlEl.textContent = job.name;
  if (sub) sub.textContent = (job.name ? job.url + ' · ' : '') + jobSubText(job);
  const pct = jobProgress(job);
  if (fill) fill.style.width = pct + '%';

  // Show zip button when files are ready
  if (job.has_files && actions && !actions.querySelector('.btn-zip-job')) {
    const zipBtn = document.createElement('button');
    zipBtn.className = 'btn-icon btn-zip-job';
    zipBtn.title = 'Download as ZIP';
    zipBtn.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7,10 12,15 17,10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>`;
    zipBtn.addEventListener('click', e => { e.stopPropagation(); window.location.href = `/api/jobs/${job.job_id}/zip`; });
    actions.prepend(zipBtn);
  }

  // Sync all track rows (add new ones, update existing)
  const tracksEl = card.querySelector('.job-tracks');
  if (tracksEl && job.tracks) {
    job.tracks.forEach(t => {
      const rowId = `track-${job.job_id}-${t.id}`;
      const existing = document.getElementById(rowId);
      if (!existing) {
        const row = document.createElement('div');
        row.className = 'track-row';
        row.id = rowId;
        row.innerHTML = trackRowHTML(job.job_id, t);
        tracksEl.appendChild(row);
      } else {
        // Update dot and progress from authoritative server state
        const dot  = existing.querySelector('.track-dot');
        const prog = existing.querySelector('.track-progress');
        const status = t.status || 'queued';
        if (dot) dot.className = `track-dot ${status}`;
        if (prog) {
          prog.textContent = status === 'done' ? '✓' : status === 'error' ? '✗' : (existing.querySelector('.track-progress')?.textContent || '');
          prog.className = 'track-progress' + (status === 'error' ? ' error' : '');
        }
      }
    });
  }
}

function jobCardHTML(job) {
  const pct = jobProgress(job);
  const tracksHTML = (job.tracks || []).map(t => `
    <div class="track-row" id="track-${job.job_id}-${t.id}">
      ${trackRowHTML(job.job_id, t)}
    </div>`).join('');

  return `
    <div class="job-header">
      <div class="job-status-icon ${job.status}">${statusIcon(job.status)}</div>
      <div class="job-info">
        <div class="job-url">${escHtml(job.name || job.url)}</div>
        <div class="job-sub">${job.name ? escHtml(job.url) + ' · ' : ''}${jobSubText(job)}</div>
      </div>
      <div class="job-actions">
        ${job.has_files ? `
        <button class="btn-icon btn-zip-job" title="Download as ZIP">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7,10 12,15 17,10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
        </button>` : ''}
        <button class="btn-icon btn-delete-job danger" title="Remove">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3,6 5,6 21,6"/><path d="M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/><path d="M9 6V4h6v2"/></svg>
        </button>
      </div>
    </div>
    <div class="job-progress-bar"><div class="job-progress-fill" style="width:${pct}%"></div></div>
    <div class="job-tracks">${tracksHTML}</div>`;
}

function trackRowHTML(jobID, t) {
  const prog = t.status === 'done' ? '✓' : t.status === 'error' ? '✗' : '';
  const progClass = 'track-progress' + (t.status === 'error' ? ' error' : '');
  return `
    <div class="track-dot ${t.status || 'queued'}"></div>
    <div class="track-title"><strong>${escHtml(t.artist)}</strong> — ${escHtml(t.title)}</div>
    <div class="${progClass}">${prog}</div>`;
}

function statusIcon(status) {
  switch (status) {
    case 'running':
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="animation:spin 1s linear infinite"><path d="M21 12a9 9 0 11-18 0 9 9 0 0118 0"/></svg>`;
    case 'done':
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20,6 9,17 4,12"/></svg>`;
    case 'error':
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>`;
    default:
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10" stroke-dasharray="4 2"/></svg>`;
  }
}

function jobSubText(job) {
  if (job.status === 'pending')  return 'Waiting…';
  if (job.status === 'running')  return `Downloading… ${job.completed + job.failed} / ${job.total || '?'} done`;
  if (job.status === 'done')     return `${job.completed} downloaded${job.failed ? `, ${job.failed} failed` : ''}`;
  if (job.status === 'error') {
    const msg = job.tracks?.[0]?.message;
    return msg ? `Error: ${msg}` : 'Failed';
  }
  return job.status;
}

function jobProgress(job) {
  if (job.total === 0) return job.status === 'done' ? 100 : 0;
  return Math.round(((job.completed + job.failed) / job.total) * 100);
}

function updateQueueBadge() {
  const active = Object.values(state.jobs).filter(j => j.status === 'running' || j.status === 'pending').length;
  const badge = $('#queue-badge');
  if (active > 0) { badge.textContent = active; badge.style.display = ''; }
  else badge.style.display = 'none';
}

// ─── Recent (download view) ────────────────────────────────────────────────────
function renderRecent() {
  const section = $('#recent-section');
  const list = $('#recent-list');
  const jobs = Object.values(state.jobs).slice(0, 5);
  if (jobs.length === 0) { section.style.display = 'none'; return; }
  section.style.display = '';
  list.innerHTML = jobs.map(j => `
    <div class="recent-item" data-job="${j.job_id}">
      <div class="recent-status ${j.status}"></div>
      <div class="recent-info">
        <div class="recent-url">${escHtml(j.url)}</div>
        <div class="recent-meta">${jobSubText(j)}</div>
      </div>
      <div class="recent-counts">${j.completed}/${j.total || '?'}</div>
    </div>`).join('');

  // Click to jump to queue
  $$('.recent-item', list).forEach(item => {
    item.addEventListener('click', () => {
      $$('.nav-item').forEach(b => b.classList.remove('active'));
      $$('.view').forEach(v => v.classList.remove('active'));
      $('[data-view="queue"]').classList.add('active');
      $('#view-queue').classList.add('active');
    });
  });
}

// ─── Fix Tags ─────────────────────────────────────────────────────────────────
function initFix() {
  $('#btn-fix')?.addEventListener('click', runFix);
  $('#btn-clear-log')?.addEventListener('click', () => {
    $('#fix-log-content').textContent = '';
    $('#fix-log').style.display = 'none';
  });
}

async function runFix() {
  const dir = $('#fix-dir-input').value.trim();
  const btn = $('#btn-fix');
  const log = $('#fix-log');
  const content = $('#fix-log-content');

  btn.disabled = true;
  log.style.display = '';
  content.textContent = 'Running…\n';

  try {
    const res = await fetch('/api/fix', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ dir }),
    });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Fix failed');
    content.textContent += (data.messages || []).join('\n') + '\n\n[Done]';
    toast('Fix complete');
  } catch (e) {
    content.textContent += '\n[ERROR] ' + e.message;
    toast(e.message, 'error');
  } finally {
    btn.disabled = false;
    content.scrollTop = content.scrollHeight;
  }
}

function appendFixLog(msg) {
  const content = $('#fix-log-content');
  if (!content) return;
  $('#fix-log').style.display = '';
  content.textContent += msg + '\n';
  content.scrollTop = content.scrollHeight;
}

// ─── Toast ────────────────────────────────────────────────────────────────────
function toast(msg, type = 'info') {
  const container = $('#toast-container');
  const el = document.createElement('div');
  el.className = `toast${type !== 'info' ? ' ' + type : ''}`;
  el.textContent = msg;
  container.appendChild(el);
  setTimeout(() => el.remove(), 4000);
}

// ─── Utils ────────────────────────────────────────────────────────────────────
function escHtml(str) {
  if (!str) return '';
  return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

// Add spin keyframe to styles
const spinStyle = document.createElement('style');
spinStyle.textContent = `@keyframes spin { to { transform: rotate(360deg); } }`;
document.head.appendChild(spinStyle);

// ─── Load existing jobs ───────────────────────────────────────────────────────
async function loadJobs() {
  try {
    const res = await fetch('/api/jobs');
    if (!res.ok) return;
    const jobs = await res.json();
    (jobs || []).forEach(j => { state.jobs[j.job_id] = j; });
    renderQueue();
    renderRecent();
    updateQueueBadge();
  } catch (_) {}
}

// ─── Polling fallback (catches missed WS messages) ────────────────────────────
function startPolling() {
  setInterval(async () => {
    const hasActive = Object.values(state.jobs).some(
      j => j.status === 'running' || j.status === 'pending'
    );
    // Always poll — ensures state is fresh even after WS reconnects
    await loadJobs();
  }, 3000);
}

// ─── Init ─────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  initNav();
  initSettingsForm();
  initDownload();
  initSearch();
  initFix();
  connectWS();
  loadSettings();
  loadJobs();
  startPolling();
});
