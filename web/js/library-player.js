/**
 * Library detail audio player: mix + stem waveforms with HTML5 audio.
 * Overlays: beat grid, section labels, energy strip, spectrogram, issue seek.
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

  function drawPeaks(canvas, peaks, progress, overlays) {
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
    const duration = (overlays && overlays.duration) || 0;

    // Section backgrounds
    if (overlays && overlays.sections && duration > 0) {
      overlays.sections.forEach((sec, i) => {
        const x0 = (sec.start_time / duration) * w;
        const x1 = (sec.end_time / duration) * w;
        ctx.fillStyle = i % 2 === 0 ? 'rgba(0,229,160,0.06)' : 'rgba(100,140,255,0.06)';
        ctx.fillRect(x0, 0, Math.max(1, x1 - x0), h);
      });
    }

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

    // Estimated beat ticks
    if (overlays && overlays.estimatedBeats && duration > 0) {
      ctx.strokeStyle = 'rgba(255,255,255,0.22)';
      ctx.lineWidth = Math.max(1, dpr);
      overlays.estimatedBeats.forEach(t => {
        const x = (t / duration) * w;
        ctx.beginPath();
        ctx.moveTo(x, h * 0.15);
        ctx.lineTo(x, h * 0.85);
        ctx.stroke();
      });
    }
    // Measured beat ticks
    if (overlays && overlays.measuredBeats && duration > 0) {
      ctx.strokeStyle = 'rgba(255,200,80,0.85)';
      ctx.lineWidth = Math.max(1, dpr);
      overlays.measuredBeats.forEach(t => {
        const x = (t / duration) * w;
        ctx.beginPath();
        ctx.moveTo(x, 0);
        ctx.lineTo(x, h);
        ctx.stroke();
      });
    }
    // Section boundary markers + labels
    if (overlays && overlays.sections && duration > 0) {
      overlays.sections.forEach(sec => {
        const x = (sec.start_time / duration) * w;
        ctx.fillStyle = 'rgba(120,180,255,0.9)';
        ctx.fillRect(x, 0, Math.max(1, dpr), h);
        if (sec.label) {
          ctx.fillStyle = 'rgba(200,220,255,0.9)';
          ctx.font = `${Math.max(10, 11 * dpr)}px sans-serif`;
          ctx.fillText(sec.label, x + 3 * dpr, 12 * dpr);
        }
      });
    }

    if (playheadX >= 0) {
      ctx.fillStyle = waveColor;
      ctx.fillRect(playheadX, 0, Math.max(1, dpr), h);
    }
  }

  function drawEnergy(canvas, energy, progress) {
    if (!canvas || !energy || !energy.energy_0_100 || !energy.energy_0_100.length) return;
    const dpr = window.devicePixelRatio || 1;
    const cssW = canvas.clientWidth || 600;
    const cssH = canvas.clientHeight || 36;
    const w = Math.max(1, Math.floor(cssW * dpr));
    const h = Math.max(1, Math.floor(cssH * dpr));
    if (canvas.width !== w || canvas.height !== h) {
      canvas.width = w;
      canvas.height = h;
    }
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, w, h);
    const vals = energy.energy_0_100;
    const barW = Math.max(1, w / vals.length);
    const accent = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim() || '#00e5a0';
    for (let i = 0; i < vals.length; i++) {
      const frac = Math.min(1, Math.max(0, vals[i] / 100));
      const bh = Math.max(1, frac * h * 0.92);
      const x = (i / vals.length) * w;
      ctx.fillStyle = progress != null && x / w <= progress ? accent : 'rgba(255,255,255,0.25)';
      ctx.fillRect(x, h - bh, Math.max(1, barW - dpr), bh);
    }
    if (progress != null) {
      ctx.fillStyle = accent;
      ctx.fillRect(progress * w, 0, Math.max(1, dpr), h);
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

  function mixOverlays() {
    if (!active) return null;
    const r = active.research || {};
    return {
      duration: active.durations.mix || r.rms?.duration_sec || 0,
      measuredBeats: r.beat_times_measured || [],
      estimatedBeats: r.beat_grid_estimated || [],
      sections: r.labeled_sections || [],
    };
  }

  function redrawMix(progress) {
    if (!active) return;
    const canvas = active.canvases.mix;
    if (canvas && active.peakData.mix) {
      drawPeaks(canvas, active.peakData.mix, progress, mixOverlays());
    }
    const energyCanvas = active.root.querySelector('[data-canvas="energy"]');
    if (energyCanvas && active.research && active.research.energy) {
      drawEnergy(energyCanvas, active.research.energy, progress);
    }
  }

  function tick() {
    if (!active || !active.current) return;
    const a = active.audios[active.current];
    if (!a) return;
    const dur = a.duration || active.durations[active.current] || 0;
    const t = a.currentTime || 0;
    const prog = dur > 0 ? t / dur : 0;
    if (active.current === 'mix') {
      redrawMix(prog);
    } else {
      const canvas = active.canvases[active.current];
      if (canvas && active.peakData[active.current]) {
        drawPeaks(canvas, active.peakData[active.current], prog, null);
      }
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
    const canvas = active.canvases[name] || active.root.querySelector(`[data-canvas="${name}"]`);
    const a = active.audios.mix;
    if (!canvas || !a) return;
    const rect = canvas.getBoundingClientRect();
    const ratio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
    const dur = a.duration || active.durations.mix || 0;
    if (dur > 0) {
      a.currentTime = ratio * dur;
      active.current = 'mix';
      redrawMix(ratio);
      const timeEl = active.root.querySelector('[data-time="mix"]');
      if (timeEl) timeEl.textContent = `${fmtTime(a.currentTime)} / ${fmtTime(dur)}`;
    }
  }

  function seekTo(sec) {
    if (!active || !active.audios.mix) return;
    const a = active.audios.mix;
    const dur = a.duration || active.durations.mix || 0;
    if (!(dur > 0)) return;
    a.currentTime = Math.min(dur, Math.max(0, sec));
    active.current = 'mix';
    redrawMix(a.currentTime / dur);
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

  function applyResearch(research) {
    if (!active) return;
    active.research = research || {};
    redrawMix(0);

    const energyWrap = active.root.querySelector('#library-energy-wrap');
    if (energyWrap) {
      if (research && research.energy && research.energy.status === 'measured') {
        energyWrap.style.display = '';
        drawEnergy(active.root.querySelector('[data-canvas="energy"]'), research.energy, 0);
      } else {
        energyWrap.style.display = 'none';
      }
    }

    const issuesEl = active.root.querySelector('#library-issues-list');
    if (issuesEl) {
      const issues = (research && research.issues) || [];
      if (issues.length === 0) {
        issuesEl.innerHTML = '<p class="field-hint">No issues flagged.</p>';
      } else {
        issuesEl.innerHTML = issues.map((iss, idx) => {
          const t = iss.start_time != null ? fmtTime(iss.start_time) : '—';
          const seek = iss.start_time != null
            ? ` <button type="button" class="btn-secondary library-issue-seek" data-issue-idx="${idx}">Seek</button>`
            : '';
          return `<div class="library-issue" data-issue-idx="${idx}">
            <div class="library-issue-head"><strong>${esc(iss.category || 'issue')}</strong>
              <span class="library-issue-meta">${esc(iss.reliability || '')} · ${esc(t)}</span>${seek}</div>
            <div class="library-issue-body">${esc(iss.measurement || '')} ${esc(iss.units || '')} — ${esc(iss.suggested_action || '')}</div>
          </div>`;
        }).join('');
        issuesEl.querySelectorAll('.library-issue-seek').forEach(btn => {
          btn.addEventListener('click', e => {
            e.stopPropagation();
            const idx = Number(btn.dataset.issueIdx);
            const iss = issues[idx];
            if (iss && iss.start_time != null) seekTo(iss.start_time);
          });
        });
      }
    }

    const metaExtra = active.root.querySelector('#library-research-meta');
    if (metaExtra && research) {
      const parts = [];
      if (research.camelot) parts.push(`Camelot ${research.camelot}`);
      if (research.tempo_half_bpm) parts.push(`½ ${Number(research.tempo_half_bpm).toFixed(1)} / 2× ${Number(research.tempo_double_bpm).toFixed(1)} BPM`);
      if (research.clipping_status === 'measured') parts.push(`Clipping runs: ${(research.clipping || []).length}`);
      if (research.labeled_sections) parts.push(`Sections: ${research.labeled_sections.length}`);
      metaExtra.textContent = parts.join(' · ') || '';
    }
  }

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
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
          <p class="field-hint library-overlay-legend">Yellow ticks = measured beats · faint ticks = estimated grid · blue = section boundaries</p>
        </div>
        <div class="library-wave-row" id="library-energy-wrap" style="display:none">
          <div class="library-wave-meta">
            <span class="analysis-metric-label">Energy (within-track RMS %ile)</span>
          </div>
          <canvas class="library-wave-canvas library-energy-canvas" data-canvas="energy"></canvas>
        </div>
        <div class="library-spectrogram-wrap">
          <div class="library-wave-meta"><span class="analysis-metric-label">Spectrogram</span></div>
          <div class="library-spectrogram-status" id="library-spec-status">Loading spectrogram…</div>
          <img class="library-spectrogram" id="library-spectrogram" alt="Spectrogram" style="display:none" />
        </div>
        <p class="library-research-meta" id="library-research-meta"></p>
        <div class="library-issues">
          <div class="analysis-metric-label">Issues</div>
          <div id="library-issues-list"><p class="field-hint">Loading research…</p></div>
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
      research: null,
      id,
    };

    playerRoot.querySelectorAll('[data-play]').forEach(btn => {
      btn.addEventListener('click', e => {
        e.stopPropagation();
        togglePlay(btn.dataset.play);
      });
    });
    playerRoot.querySelectorAll('[data-canvas="mix"], [data-canvas="energy"]').forEach(canvas => {
      canvas.addEventListener('click', e => {
        e.stopPropagation();
        seekFromClick('mix', e);
      });
    });
    playerRoot.querySelectorAll('[data-canvas]').forEach(canvas => {
      const name = canvas.dataset.canvas;
      if (name === 'mix' || name === 'energy') return;
      canvas.addEventListener('click', e => {
        e.stopPropagation();
        const a = audios[name];
        if (!a) return;
        const rect = canvas.getBoundingClientRect();
        const ratio = Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width));
        const dur = a.duration || durations[name] || 0;
        if (dur > 0) {
          a.currentTime = ratio * dur;
          active.current = name;
          drawPeaks(canvas, peakData[name] || [], ratio, null);
        }
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
        if (name === 'mix') redrawMix(1);
        else {
          const canvas = canvases[name];
          if (canvas && peakData[name]) drawPeaks(canvas, peakData[name], 1, null);
        }
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
          redrawMix(0);
        }
        const stemPeaks = data.stems || {};
        stemNames.forEach(n => {
          const p = stemPeaks[n];
          if (!p || !p.peaks) return;
          peakData[n] = p.peaks;
          if (p.duration_sec) durations[n] = p.duration_sec;
          const c = canvases[n];
          if (c) drawPeaks(c, peakData[n], 0, null);
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

    const specImg = playerRoot.querySelector('#library-spectrogram');
    const specStatus = playerRoot.querySelector('#library-spec-status');
    const specURL = `/api/audio/library/${id}/spectrogram`;
    const img = new Image();
    img.onload = () => {
      if (!active || active.root !== playerRoot) return;
      if (specStatus) specStatus.style.display = 'none';
      if (specImg) {
        specImg.src = specURL;
        specImg.style.display = '';
      }
    };
    img.onerror = () => {
      if (!active || active.root !== playerRoot) return;
      if (specStatus) {
        specStatus.textContent = 'Spectrogram unavailable';
        specStatus.classList.add('library-wave-status-error');
      }
    };
    img.src = specURL;

    fetch(`/api/audio/library/${id}/research`)
      .then(async res => {
        const data = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(data.error || 'Research failed');
        return data;
      })
      .then(data => {
        if (!active || active.root !== playerRoot) return;
        applyResearch(data);
      })
      .catch(err => {
        if (!active || active.root !== playerRoot) return;
        const issuesEl = playerRoot.querySelector('#library-issues-list');
        if (issuesEl) issuesEl.innerHTML = `<p class="field-hint library-wave-status-error">${esc(err.message || 'Research unavailable')}</p>`;
      });
  }

  global.LibraryPlayer = { mount, destroy, seekTo };
})(window);
