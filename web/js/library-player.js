/**
 * Library detail audio player: mix + stem waveforms with HTML5 audio.
 * Loaded before app.js; exposes window.LibraryPlayer.
 */
(function (global) {
  'use strict';

  let active = null;

  function destroy() {
    if (!active) return;
    if (active.raf) cancelAnimationFrame(active.raf);
    Object.values(active.audios || {}).forEach(a => {
      try {
        a.pause();
        a.removeAttribute('src');
        a.load();
      } catch (_) {}
    });
    active = null;
  }

  function fmtTime(sec) {
    if (sec == null || !Number.isFinite(sec) || sec < 0) return '0:00';
    const s = Math.floor(sec);
    const m = Math.floor(s / 60);
    const r = s % 60;
    return m + ':' + String(r).padStart(2, '0');
  }

  function drawPeaks(canvas, peaks, progress) {
    if (!canvas || !peaks || !peaks.length) return;
    const dpr = window.devicePixelRatio || 1;
    const cssW = canvas.clientWidth || 600;
    const cssH = canvas.clientHeight || 64;
    const w = Math.max(1, Math.floor(cssW * dpr));
    const h = Math.max(1, Math.floor(cssH * dpr));
    if (canvas.width !== w || canvas.height !== h) {
      canvas.width = w;
      canvas.height = h;
    }
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, w, h);
    const mid = h / 2;
    const pairs = Math.floor(peaks.length / 2);
    const barW = Math.max(1, w / pairs);
    const waveColor = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim() || '#00e5a0';
    const muted = 'rgba(255,255,255,0.18)';
    const playheadX = progress != null ? progress * w : -1;

    for (let i = 0; i < pairs; i++) {
      const mn = peaks[i * 2];
      const mx = peaks[i * 2 + 1];
      const x = (i / pairs) * w;
      const y1 = mid - mx * mid * 0.92;
      const y2 = mid - mn * mid * 0.92;
      const barH = Math.max(1, y2 - y1);
      ctx.fillStyle = playheadX >= 0 && x <= playheadX ? waveColor : muted;
      ctx.fillRect(x, y1, Math.max(1, barW - dpr), barH);
    }
    if (playheadX >= 0) {
      ctx.fillStyle = waveColor;
      ctx.fillRect(playheadX, 0, Math.max(1, dpr), h);
    }
  }

  function setPlaying(name, playing) {
    if (!active) return;
    const btn = active.root.querySelector(`[data-play="${name}"]`);
    if (btn) btn.textContent = playing ? 'Pause' : 'Play';
  }

  function stopAllExcept(keep) {
    if (!active) return;
    Object.entries(active.audios).forEach(([name, a]) => {
      if (name === keep) return;
      a.pause();
      setPlaying(name, false);
    });
  }

  function tick() {
    if (!active || !active.current) return;
    const a = active.audios[active.current];
    if (!a) return;
    const dur = a.duration || active.durations[active.current] || 0;
    const t = a.currentTime || 0;
    const prog = dur > 0 ? t / dur : 0;
    const canvas = active.canvases[active.current];
    if (canvas && active.peakData[active.current]) {
      drawPeaks(canvas, active.peakData[active.current], prog);
    }
    const timeEl = active.root.querySelector(`[data-time="${active.current}"]`);
    if (timeEl) timeEl.textContent = `${fmtTime(t)} / ${fmtTime(dur)}`;
    if (!a.paused) {
      active.raf = requestAnimationFrame(tick);
    } else {
      active.raf = null;
      setPlaying(active.current, false);
    }
  }

  function seekFromClick(name, e) {
    if (!active) return;
    const canvas = active.canvases[name];
    const a = active.audios[name];
    if (!canvas || !a) return;
    const rect = canvas.getBoundingClientRect();
    const ratio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
    const dur = a.duration || active.durations[name] || 0;
    if (dur > 0) {
      a.currentTime = ratio * dur;
      active.current = name;
      drawPeaks(canvas, active.peakData[name] || [], ratio);
      const timeEl = active.root.querySelector(`[data-time="${name}"]`);
      if (timeEl) timeEl.textContent = `${fmtTime(a.currentTime)} / ${fmtTime(dur)}`;
    }
  }

  function togglePlay(name) {
    if (!active) return;
    const a = active.audios[name];
    if (!a) return;
    if (!a.paused) {
      a.pause();
      setPlaying(name, false);
      if (active.raf) {
        cancelAnimationFrame(active.raf);
        active.raf = null;
      }
      return;
    }
    stopAllExcept(name);
    active.current = name;
    a.play().then(() => {
      setPlaying(name, true);
      if (active.raf) cancelAnimationFrame(active.raf);
      active.raf = requestAnimationFrame(tick);
    }).catch(() => {
      setPlaying(name, false);
    });
  }

  function mount(root, opts) {
    destroy();
    if (!root || !opts || !opts.id) return;

    const id = opts.id;
    const stems = opts.stems || {};
    const stemNames = ['vocals', 'drums', 'bass', 'other'].filter(n => stems[n]);

    let stemsHTML = '';
    if (stemNames.length === 0) {
      stemsHTML = '<p class="field-hint library-stems-hint">Split stems from the Stems panel to see stem waveforms.</p>';
    } else {
      stemsHTML = stemNames.map(n => `
        <div class="library-wave-row library-wave-stem" data-track="${n}">
          <div class="library-wave-meta">
            <span class="analysis-metric-label">${n}</span>
            <button type="button" class="btn-secondary library-play-btn" data-play="${n}">Play</button>
            <span class="library-wave-time" data-time="${n}">0:00 / 0:00</span>
          </div>
          <canvas class="library-wave-canvas library-wave-canvas-stem" data-canvas="${n}"></canvas>
        </div>`).join('');
    }

    root.innerHTML = `
      <div class="library-player" id="library-player">
        <div class="library-wave-row library-wave-mix" data-track="mix">
          <div class="library-wave-meta">
            <span class="analysis-metric-label">Mix</span>
            <button type="button" class="btn-secondary library-play-btn" data-play="mix">Play</button>
            <span class="library-wave-time" data-time="mix">0:00 / 0:00</span>
          </div>
          <div class="library-wave-status" id="library-wave-status">Loading waveform…</div>
          <canvas class="library-wave-canvas" data-canvas="mix" style="display:none"></canvas>
        </div>
        <div class="library-stems-block">${stemsHTML}</div>
      </div>`;

    const playerRoot = root.querySelector('#library-player');
    const audios = {};
    const canvases = {};
    const peakData = {};
    const durations = {};

    const mixAudio = new Audio(`/api/audio/library/${id}/file`);
    mixAudio.preload = 'metadata';
    audios.mix = mixAudio;
    canvases.mix = playerRoot.querySelector('[data-canvas="mix"]');

    stemNames.forEach(n => {
      const a = new Audio(`/api/audio/library/${id}/stems/${n}`);
      a.preload = 'metadata';
      audios[n] = a;
      canvases[n] = playerRoot.querySelector(`[data-canvas="${n}"]`);
    });

    active = {
      root: playerRoot,
      audios,
      canvases,
      peakData,
      durations,
      current: null,
      raf: null,
    };

    playerRoot.querySelectorAll('[data-play]').forEach(btn => {
      btn.addEventListener('click', e => {
        e.stopPropagation();
        togglePlay(btn.dataset.play);
      });
    });
    playerRoot.querySelectorAll('[data-canvas]').forEach(canvas => {
      canvas.addEventListener('click', e => {
        e.stopPropagation();
        seekFromClick(canvas.dataset.canvas, e);
      });
    });

    Object.entries(audios).forEach(([name, a]) => {
      a.addEventListener('loadedmetadata', () => {
        durations[name] = a.duration;
        const timeEl = playerRoot.querySelector(`[data-time="${name}"]`);
        if (timeEl) timeEl.textContent = `0:00 / ${fmtTime(a.duration)}`;
      });
      a.addEventListener('ended', () => {
        setPlaying(name, false);
        if (active && active.current === name && active.raf) {
          cancelAnimationFrame(active.raf);
          active.raf = null;
        }
        const canvas = canvases[name];
        if (canvas && peakData[name]) drawPeaks(canvas, peakData[name], 1);
      });
    });

    fetch(`/api/audio/library/${id}/waveforms`)
      .then(async res => {
        const data = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(data.error || 'Failed to load waveforms');
        return data;
      })
      .then(data => {
        if (!active || active.root !== playerRoot) return;
        const status = playerRoot.querySelector('#library-wave-status');
        if (status) status.style.display = 'none';
        const mixCanvas = canvases.mix;
        if (mixCanvas && data.mix && data.mix.peaks) {
          peakData.mix = data.mix.peaks;
          if (data.mix.duration_sec) durations.mix = data.mix.duration_sec;
          mixCanvas.style.display = '';
          drawPeaks(mixCanvas, peakData.mix, 0);
        }
        const stemPeaks = data.stems || {};
        stemNames.forEach(n => {
          const p = stemPeaks[n];
          if (!p || !p.peaks) return;
          peakData[n] = p.peaks;
          if (p.duration_sec) durations[n] = p.duration_sec;
          const c = canvases[n];
          if (c) drawPeaks(c, peakData[n], 0);
        });
      })
      .catch(err => {
        if (!active || active.root !== playerRoot) return;
        const status = playerRoot.querySelector('#library-wave-status');
        if (status) {
          status.textContent = err.message || 'Waveforms unavailable';
          status.classList.add('library-wave-status-error');
        }
      });
  }

  global.LibraryPlayer = { mount, destroy };
})(window);
