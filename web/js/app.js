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
  searchPerPage: 50,
  searchGenreId: 0,
  searchGenres: [],
  searchIncludeArtists: false,
  searchTopTracks: false,
  searchController: null,
  searchResults: null,
  searchTrackSort: { column: 'released', dir: 'desc' },
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

function initSearch() {
  const input = $('#search-input');
  const btnSearch = $('#btn-search');
  const tabs = $('#search-tabs');
  const perPage = $('#search-per-page');
  const genreSelect = $('#search-genre');
  const includeArtists = $('#search-include-artists');
  const topTracks = $('#search-top-tracks');

  loadSearchGenres();

  function syncSearchOptionsUI() {
    const tracksOnly = state.searchType === 'tracks';
    const artistsOnly = state.searchType === 'artists';
    $('#search-include-artists-wrap').style.display = tracksOnly ? '' : 'none';
    $('#search-top-tracks-wrap').style.display = (artistsOnly || state.searchType === 'all' || (tracksOnly && state.searchIncludeArtists)) ? '' : 'none';
    if (!tracksOnly) {
      includeArtists.checked = false;
      state.searchIncludeArtists = false;
    }
    if (artistsOnly || state.searchType === 'all') {
      topTracks.disabled = false;
    } else if (tracksOnly && !state.searchIncludeArtists) {
      topTracks.checked = false;
      state.searchTopTracks = false;
      topTracks.disabled = true;
    }
  }

  perPage?.addEventListener('change', () => {
    state.searchPerPage = parseInt(perPage.value, 10) || 50;
    triggerSearchIfReady();
  });

  genreSelect?.addEventListener('change', () => {
    state.searchGenreId = parseInt(genreSelect.value, 10) || 0;
    triggerSearchIfReady();
  });

  includeArtists?.addEventListener('change', () => {
    state.searchIncludeArtists = includeArtists.checked;
    syncSearchOptionsUI();
    triggerSearchIfReady();
  });

  topTracks?.addEventListener('change', () => {
    state.searchTopTracks = topTracks.checked;
    triggerSearchIfReady();
  });

  tabs?.addEventListener('click', (e) => {
    const pill = e.target.closest('.pill');
    if (!pill) return;
    $$('.pill', tabs).forEach(p => p.classList.remove('active'));
    pill.classList.add('active');
    state.searchType = pill.dataset.type;
    syncSearchOptionsUI();
    triggerSearchIfReady();
  });

  syncSearchOptionsUI();
  state.searchPerPage = parseInt(perPage?.value, 10) || 50;

  btnSearch?.addEventListener('click', () => startSearch());

  input?.addEventListener('input', () => {
    const q = input.value.trim();
    if (q.length < 2 && !state.searchGenreId) {
      state.searchQuery = '';
      renderSearchEmpty('Enter a search term or pick a genre, then click Search');
      setSearchStatus('');
    }
  });

  input?.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      e.preventDefault();
      startSearch();
    }
  });
}

function startSearch() {
  const input = $('#search-input');
  const q = input?.value.trim() || '';
  const hasQuery = q.length >= 2;
  if (!hasQuery && !state.searchGenreId) {
    toast('Enter at least 2 characters or pick a genre', 'warn');
    return;
  }
  runSearch(q);
}

async function loadSearchGenres() {
  const select = $('#search-genre');
  if (!select) return;
  try {
    const res = await fetch('/api/genres');
    const data = await res.json();
    if (!res.ok) return;
    state.searchGenres = data || [];
    const current = select.value;
    select.innerHTML = '<option value="">All genres</option>' +
      state.searchGenres.map(g => `<option value="${g.id}">${escHtml(g.name)}</option>`).join('');
    if (current) select.value = current;
  } catch (_) {}
}

function triggerSearchIfReady() {
  if (state.searchQuery.length >= 2 || state.searchGenreId) {
    runSearch(state.searchQuery || $('#search-input')?.value.trim() || '');
  }
}

async function runSearch(query) {
  const hasQuery = query.length >= 2;
  if (!hasQuery && !state.searchGenreId) return;

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
      per_page: String(state.searchPerPage),
    });
    if (state.searchIncludeArtists && state.searchType === 'tracks') {
      params.set('include_artists', '1');
    }
    if (state.searchTopTracks) {
      params.set('top_tracks', '1');
    }
    if (state.searchGenreId) {
      params.set('genre_id', String(state.searchGenreId));
    }
    const res = await fetch(`/api/search?${params}`, { signal: state.searchController.signal });
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || 'Search failed');

    renderSearchResults(data);
    const counts = {
      artists: data.artists?.items?.length || 0,
      releases: data.releases?.items?.length || 0,
      tracks: data.tracks?.items?.length || 0,
      labels: data.labels?.items?.length || 0,
      charts: data.charts?.items?.length || 0,
    };
    const total = Object.values(counts).reduce((a, b) => a + b, 0);
    if (total === 0) {
      setSearchStatus('No results found', 'empty');
    } else {
      const parts = [];
      if (counts.artists) parts.push(`${counts.artists} artist${counts.artists !== 1 ? 's' : ''}`);
      if (counts.releases) parts.push(`${counts.releases} release${counts.releases !== 1 ? 's' : ''}`);
      if (counts.tracks) parts.push(`${counts.tracks} track${counts.tracks !== 1 ? 's' : ''}`);
      if (counts.labels) parts.push(`${counts.labels} label${counts.labels !== 1 ? 's' : ''}`);
      if (counts.charts) parts.push(`${counts.charts} chart${counts.charts !== 1 ? 's' : ''}`);
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

function searchSectionHeading(title, count) {
  const n = count ?? 0;
  return `<h2 class="search-section-title">${escHtml(title)} <span class="search-section-count">${n}</span></h2>`;
}

function renderSearchResults(data) {
  state.searchResults = data;
  const resultsEl = $('#search-results');
  const sections = [];

  if (data.artists?.items?.length) {
    const topTracksBlocks = data.artists.items
      .filter(a => a.top_tracks?.length)
      .map(a => searchArtistTopTracksHTML(a))
      .join('');
    sections.push(`
      <div class="search-section" data-section="artists">
        ${searchSectionHeading('Artists', data.artists.count || data.artists.items.length)}
        <div class="artist-card-row">
          ${data.artists.items.map(a => searchArtistCardHTML(a)).join('')}
        </div>
        ${topTracksBlocks}
      </div>`);
  }

  if (data.releases?.items?.length) {
    sections.push(`
      <div class="search-section" data-section="releases">
        ${searchSectionHeading('Releases', data.releases.count || data.releases.items.length)}
        <div class="search-list">
          ${data.releases.items.map(r => searchReleaseHTML(r)).join('')}
        </div>
      </div>`);
  }

  if (data.tracks?.items?.length) {
    const tracks = sortTrackItems(data.tracks.items, state.searchTrackSort.column, state.searchTrackSort.dir);
    sections.push(`
      <div class="search-section" data-section="tracks">
        ${searchSectionHeading('Tracks', data.tracks.count || data.tracks.items.length)}
        ${searchTrackTableHTML(tracks, 'search-tracks-table')}
      </div>`);
  }

  if (data.labels?.items?.length) {
    sections.push(`
      <div class="search-section" data-section="labels">
        ${searchSectionHeading('Labels', data.labels.count || data.labels.items.length)}
        <div class="search-list">
          ${data.labels.items.map(l => searchLabelHTML(l)).join('')}
        </div>
      </div>`);
  }

  if (data.charts?.items?.length) {
    sections.push(`
      <div class="search-section" data-section="charts">
        ${searchSectionHeading('Charts', data.charts.count || data.charts.items.length)}
        <div class="search-list">
          ${data.charts.items.map(c => searchChartHTML(c)).join('')}
        </div>
      </div>`);
  }

  if (sections.length === 0) {
    state.searchResults = null;
    renderSearchEmpty('No results found');
    return;
  }

  resultsEl.innerHTML = sections.join('');
  bindSearchActions(resultsEl);
  bindArtistCards(resultsEl);
  bindTrackSortHeaders(resultsEl);
}

function sortTrackItems(items, column, dir) {
  const sorted = [...items];
  const mult = dir === 'asc' ? 1 : -1;

  sorted.sort((a, b) => {
    let av;
    let bv;
    switch (column) {
      case 'title':
        av = (a.title || '').toLowerCase();
        bv = (b.title || '').toLowerCase();
        break;
      case 'artists':
        av = (a.artists || '').toLowerCase();
        bv = (b.artists || '').toLowerCase();
        break;
      case 'label':
        av = (a.label || '').toLowerCase();
        bv = (b.label || '').toLowerCase();
        break;
      case 'genre':
        av = (a.genre || '').toLowerCase();
        bv = (b.genre || '').toLowerCase();
        break;
      case 'bpm':
        av = a.bpm || 0;
        bv = b.bpm || 0;
        break;
      case 'key':
        av = (a.key || '').toLowerCase();
        bv = (b.key || '').toLowerCase();
        break;
      case 'camelot':
        av = camelotSortValue(a.camelot);
        bv = camelotSortValue(b.camelot);
        break;
      case 'released':
        av = a.released || '';
        bv = b.released || '';
        break;
      case 'length':
        av = trackLengthSortValue(a.length);
        bv = trackLengthSortValue(b.length);
        break;
      default:
        return 0;
    }
    if (av < bv) return -1 * mult;
    if (av > bv) return 1 * mult;
    return 0;
  });
  return sorted;
}

function defaultSortDir(column) {
  if (column === 'released' || column === 'bpm' || column === 'camelot' || column === 'length') return 'desc';
  return 'asc';
}

function trackLengthSortValue(length) {
  if (!length) return 0;
  const parts = length.split(':').map(p => parseInt(p, 10));
  if (parts.some(n => Number.isNaN(n))) return 0;
  if (parts.length === 3) return parts[0] * 3600 + parts[1] * 60 + parts[2];
  if (parts.length === 2) return parts[0] * 60 + parts[1];
  return parts[0] || 0;
}

function formatTrackTime(length) {
  return length ? escHtml(length) : '—';
}

function trackThumbHTML(t) {
  return t.image_uri
    ? `<img class="search-thumb" src="${escHtml(t.image_uri)}" alt="" loading="lazy" />`
    : `<div class="search-thumb placeholder"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="3"/></svg></div>`;
}

function camelotSortValue(code) {
  const m = (code || '').match(/^(\d{1,2})([AB])$/i);
  if (!m) return 0;
  const n = parseInt(m[1], 10);
  const letter = m[2].toUpperCase() === 'B' ? 1 : 0;
  return n * 2 + letter;
}

function camelotTextHTML(code) {
  if (!code) return '—';
  const style = camelotColorStyle(code);
  return `<span class="camelot-code" style="${style}">${escHtml(code)}</span>`;
}

function camelotColorStyle(code) {
  const m = (code || '').match(/^(\d{1,2})([AB])$/i);
  if (!m) return '';
  const n = parseInt(m[1], 10);
  const minor = m[2].toUpperCase() === 'A';
  const hue = ((n - 1) * 30 + (minor ? 8 : 0)) % 360;
  const sat = minor ? 62 : 78;
  const light = minor ? 68 : 62;
  return `color:hsl(${hue},${sat}%,${light}%)`;
}

function keyCellHTML(t) {
  if (!t.camelot && !t.key) return '—';
  const camelot = t.camelot ? camelotTextHTML(t.camelot) : '';
  const name = t.key ? `<span class="musical-key-name">${escHtml(t.key)}</span>` : '';
  return `<div class="key-cell">${camelot}${name}</div>`;
}

function sortHeaderLabel(column, label) {
  const active = state.searchTrackSort.column === column;
  const arrow = active ? (state.searchTrackSort.dir === 'asc' ? ' ↑' : ' ↓') : '';
  return `<button type="button" class="search-sort-btn${active ? ' active' : ''}" data-sort="${column}" title="Sort by ${label}">${label}${arrow}</button>`;
}

function searchTrackTableHTML(tracks, tableId) {
  return `
    <div class="search-table-wrap">
      <div class="search-table search-track-table" id="${tableId || ''}">
        <div class="search-table-head search-track-row">
          <div class="search-col search-col-cover" aria-hidden="true"></div>
          <div class="search-col search-col-track">${sortHeaderLabel('title', 'Track')}</div>
          <div class="search-col search-col-artists">${sortHeaderLabel('artists', 'Artist')}</div>
          <div class="search-col search-col-label">${sortHeaderLabel('label', 'Label')}</div>
          <div class="search-col search-col-genre">${sortHeaderLabel('genre', 'Genre')}</div>
          <div class="search-col search-col-bpm">${sortHeaderLabel('bpm', 'BPM')}</div>
          <div class="search-col search-col-key">${sortHeaderLabel('camelot', 'Key')}</div>
          <div class="search-col search-col-released">${sortHeaderLabel('released', 'Released')}</div>
          <div class="search-col search-col-time">${sortHeaderLabel('length', 'Time')}</div>
          <div class="search-col search-col-actions"></div>
        </div>
        <div class="search-table-body">
          ${tracks.map(t => searchTrackRowHTML(t)).join('')}
        </div>
      </div>
    </div>`;
}

function formatReleased(dateStr) {
  if (!dateStr) return '—';
  const d = new Date(dateStr + 'T00:00:00');
  if (Number.isNaN(d.getTime())) return dateStr;
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
}

function searchTrackRowHTML(t) {
  return `
    <div class="search-item search-track-row" data-url="${escHtml(t.url)}">
      <div class="search-col search-col-cover">${trackThumbHTML(t)}</div>
      <div class="search-col search-col-track">
        <div class="search-item-info">
          <div class="search-item-title" title="${escHtml(t.title || '')}">${escHtml(t.title)}</div>
        </div>
      </div>
      <div class="search-col search-col-artists" title="${escHtml(t.artists || '')}">${escHtml(t.artists) || '—'}</div>
      <div class="search-col search-col-label" title="${escHtml(t.label || '')}">${escHtml(t.label) || '—'}</div>
      <div class="search-col search-col-genre" title="${escHtml(t.genre || '')}">${escHtml(t.genre) || '—'}</div>
      <div class="search-col search-col-bpm">${t.bpm ? escHtml(String(t.bpm)) : '—'}</div>
      <div class="search-col search-col-key">${keyCellHTML(t)}</div>
      <div class="search-col search-col-released">${formatReleased(t.released)}</div>
      <div class="search-col search-col-time">${formatTrackTime(t.length)}</div>
      <div class="search-col search-col-actions">
        <a class="btn-icon" href="${escHtml(t.url)}" target="_blank" rel="noopener" title="Open on Beatport" onclick="event.stopPropagation()">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/><polyline points="15,3 21,3 21,9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
        </a>
        <button class="btn-search-download" title="Download track">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7,10 12,15 17,10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
        </button>
      </div>
    </div>`;
}

function bindTrackSortHeaders(container) {
  $$('.search-sort-btn', container).forEach(btn => {
    btn.addEventListener('click', () => {
      const col = btn.dataset.sort;
      if (state.searchTrackSort.column === col) {
        state.searchTrackSort.dir = state.searchTrackSort.dir === 'asc' ? 'desc' : 'asc';
      } else {
        state.searchTrackSort.column = col;
        state.searchTrackSort.dir = defaultSortDir(col);
      }
      if (state.searchResults) renderSearchResults(state.searchResults);
    });
  });
}

function searchArtistCardHTML(a) {
  const media = a.image_uri
    ? `<img class="artist-card-img" src="${escHtml(a.image_uri)}" alt="" loading="lazy" />`
    : `<div class="artist-card-placeholder" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.25"><path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
      </div>`;

  return `
    <div class="artist-card" data-url="${escHtml(a.url)}" title="${escHtml(a.name)}">
      <div class="artist-card-media">
        ${media}
        <div class="artist-card-shade"></div>
        <span class="artist-card-name">${escHtml(a.name)}</span>
        <div class="artist-card-actions">
          <a class="btn-icon btn-icon-sm" href="${escHtml(a.url)}" target="_blank" rel="noopener" title="Open on Beatport" onclick="event.stopPropagation()">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/><polyline points="15,3 21,3 21,9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
          </a>
          <button class="btn-search-download btn-search-download-sm" type="button" title="Download all tracks">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7,10 12,15 17,10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          </button>
        </div>
      </div>
    </div>`;
}

function searchArtistTopTracksHTML(a) {
  return `
    <div class="artist-top-tracks-section">
      <div class="artist-top-tracks-heading">${escHtml(a.name)} · Top tracks</div>
      ${searchTrackTableHTML(sortTrackItems(a.top_tracks, state.searchTrackSort.column, state.searchTrackSort.dir), '')}
    </div>`;
}

function bindArtistCards(container) {
  $$('.artist-card', container).forEach(card => {
    card.addEventListener('click', e => {
      if (e.target.closest('.artist-card-actions')) return;
      const url = card.dataset.url;
      if (url) window.open(url, '_blank', 'noopener');
    });
  });
}

function searchCatalogCardHTML({ title, meta, image_uri, url, round, downloadTitle, showDownload = true }) {
  const img = image_uri
    ? `<img class="search-thumb${round ? ' round' : ''}" src="${escHtml(image_uri)}" alt="" loading="lazy" />`
    : `<div class="search-thumb${round ? ' round' : ''} placeholder"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 9h6v6H9z"/></svg></div>`;

  const downloadBtn = showDownload
    ? `<button class="btn-search-download" title="${escHtml(downloadTitle || 'Download')}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7,10 12,15 17,10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
      </button>`
    : '';

  return `
    <div class="search-item" data-url="${escHtml(url)}">
      <div class="search-item-main">
        ${img}
        <div class="search-item-info">
          <div class="search-item-title">${escHtml(title)}</div>
          <div class="search-item-meta">${escHtml(meta)}</div>
        </div>
        <div class="search-item-actions">
          <a class="btn-icon" href="${escHtml(url)}" target="_blank" rel="noopener" title="Open on Beatport" onclick="event.stopPropagation()">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/><polyline points="15,3 21,3 21,9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
          </a>
          ${downloadBtn}
        </div>
      </div>
    </div>`;
}

function searchReleaseHTML(r) {
  const img = r.image_uri
    ? `<img class="search-thumb" src="${escHtml(r.image_uri)}" alt="" loading="lazy" />`
    : `<div class="search-thumb placeholder"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 9h6v6H9z"/></svg></div>`;

  const metaParts = ['Release'];
  if (r.artists) metaParts.push(r.artists);
  if (r.label) metaParts.push(r.label);
  if (r.track_count) metaParts.push(`${r.track_count} tracks`);
  if (r.released) metaParts.push(formatReleased(r.released));

  const tracksHTML = (r.tracks?.length > 1)
    ? `<div class="release-nested-tracks">
        ${searchTrackTableHTML(sortTrackItems(r.tracks, state.searchTrackSort.column, state.searchTrackSort.dir), '')}
      </div>`
    : '';

  return `
    <div class="search-item search-item-artist" data-url="${escHtml(r.url)}">
      <div class="search-item-main">
        ${img}
        <div class="search-item-info">
          <div class="search-item-title">${escHtml(r.title)}</div>
          <div class="search-item-meta">${escHtml(metaParts.join(' · '))}</div>
        </div>
        <div class="search-item-actions">
          <a class="btn-icon" href="${escHtml(r.url)}" target="_blank" rel="noopener" title="Open on Beatport" onclick="event.stopPropagation()">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/><polyline points="15,3 21,3 21,9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>
          </a>
          <button class="btn-search-download" title="Download release">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7,10 12,15 17,10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          </button>
        </div>
      </div>
      ${tracksHTML}
    </div>`;
}

function searchLabelHTML(l) {
  return searchCatalogCardHTML({
    title: l.name,
    meta: 'Label',
    image_uri: l.image_uri,
    url: l.url,
    round: true,
    showDownload: false,
  });
}

function searchChartHTML(c) {
  const metaParts = ['Chart'];
  if (c.curator) metaParts.push(c.curator);
  if (c.genre) metaParts.push(c.genre);
  if (c.published) metaParts.push(formatReleased(c.published));
  return searchCatalogCardHTML({
    title: c.name,
    meta: metaParts.join(' · '),
    url: c.url,
    downloadTitle: 'Download chart',
  });
}

function bindSearchActions(container) {
  $$('.btn-search-download', container).forEach(btn => {
    btn.addEventListener('click', e => {
      e.stopPropagation();
      const url = btn.closest('.search-track-row')?.dataset.url
        || btn.closest('.search-item')?.dataset.url
        || btn.closest('.artist-card')?.dataset.url;
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
