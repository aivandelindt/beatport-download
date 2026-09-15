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
    if (active.vuRaf) cancelAnimationFrame(active.vuRaf);
    if (active.audioCtx) {
      try { active.audioCtx.close(); } catch (_) {}
    }
    if (active._onResize) {
      window.removeEventListener('resize', active._onResize);
    }
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

  function rekordboxRGB(low, mid, high, dim) {
    const gain = v => {
      const n = Number(v);
      if (!(n > 0)) return 0;
      return Math.min(1, Math.pow(n, 0.65));
    };
    const d = dim == null ? 1 : dim;
    const r = Math.round(gain(low) * 255 * d);
    const g = Math.round(gain(mid) * 255 * d);
    const b = Math.round(gain(high) * 255 * d);
    return 'rgb(' + r + ',' + g + ',' + b + ')';
  }

  // Pioneer rekordbox PERFORMANCE phrase-bar colors (Intro/Up/Down/Chorus/Verse/Bridge/Outro).
  const REKORDBOX_PHRASE = {
    intro: { fill: '#00B4E6', label: 'Intro' },
    up: { fill: '#E4007C', label: 'Up' },
    build: { fill: '#E4007C', label: 'Up' },
    down: { fill: '#2E5BFF', label: 'Down' },
    breakdown: { fill: '#2E5BFF', label: 'Down' },
    chorus: { fill: '#FF6A00', label: 'Chorus' },
    drop: { fill: '#FF6A00', label: 'Chorus' },
    verse: { fill: '#00C853', label: 'Verse' },
    bridge: { fill: '#FFD100', label: 'Bridge' },
    outro: { fill: '#9B59FF', label: 'Outro' },
    unknown: { fill: '#6B7280', label: 'Unknown' },
  };

  function phraseStyle(label) {
    const key = String(label || 'unknown').toLowerCase();
    return REKORDBOX_PHRASE[key] || REKORDBOX_PHRASE.unknown;
  }

  function viewWindow() {
    const zoom = (active && active.zoom) || 1;
    const span = 1 / zoom;
    let start = (active && active.viewStart) || 0;
    if (start < 0) start = 0;
    if (start > 1 - span) start = Math.max(0, 1 - span);
    return { start, span, end: start + span, zoom };
  }

  function timeToX(t, duration, w) {
    const { start, span } = viewWindow();
    if (!(duration > 0) || !(span > 0)) return -1;
    return ((t / duration) - start) / span * w;
  }

  const BEATS_STORAGE_KEY = 'beatportdl.libraryShowBeats';
  const ZOOM_LEVELS = [1, 2, 4, 8, 16];

  function loadShowMeasuredBeats() {
    try {
      const v = localStorage.getItem(BEATS_STORAGE_KEY);
      if (v === '0' || v === 'false') return false;
    } catch (_) {}
    return true;
  }

  function persistShowMeasuredBeats(on) {
    try {
      localStorage.setItem(BEATS_STORAGE_KEY, on ? '1' : '0');
    } catch (_) {}
  }

  function drawPeaks(canvas, wave, progress, overlays) {
    const peaks = Array.isArray(wave) ? wave : (wave && wave.peaks);
    if (!canvas || !peaks || !peaks.length) return;
    const rgbLow = !Array.isArray(wave) && Array.isArray(wave.rgb_low) ? wave.rgb_low : null;
    const rgbMid = !Array.isArray(wave) && Array.isArray(wave.rgb_mid) ? wave.rgb_mid : null;
    const rgbHigh = !Array.isArray(wave) && Array.isArray(wave.rgb_high) ? wave.rgb_high : null;
    const useRGB = rgbLow && rgbMid && rgbHigh && rgbLow.length === Math.floor(peaks.length / 2);
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
    const waveColor = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim() || '#00e5a0';
    const muted = 'rgba(255,255,255,0.18)';
    const { start, span } = viewWindow();
    const playheadX = progress != null ? ((progress - start) / span) * w : -1;
    const duration = (overlays && overlays.duration) || 0;
    const showBeats = !overlays || overlays.showMeasuredBeats !== false;

    for (let i = 0; i < pairs; i++) {
      const t0 = i / pairs;
      const t1 = (i + 1) / pairs;
      if (t1 < start || t0 > start + span) continue;
      const mn = peaks[i * 2];
      const mx = peaks[i * 2 + 1];
      const x = ((t0 - start) / span) * w;
      const x2 = ((t1 - start) / span) * w;
      const y1 = mid - mx * mid * 0.92;
      const y2 = mid - mn * mid * 0.92;
      const barH = Math.max(1, y2 - y1);
      const played = playheadX >= 0 && x <= playheadX;
      if (useRGB) {
        const dim = played ? 1 : 0.42;
        ctx.fillStyle = rekordboxRGB(rgbLow[i], rgbMid[i], rgbHigh[i], dim);
      } else {
        ctx.fillStyle = played ? waveColor : muted;
      }
      ctx.fillRect(x, y1, Math.max(1, x2 - x - dpr * 0.25), barH);
    }

    // Estimated beat ticks (skip when measured list is complete)
    if (overlays && overlays.estimatedBeats && duration > 0 && !overlays.beatsComplete) {
      ctx.strokeStyle = 'rgba(255,255,255,0.22)';
      ctx.lineWidth = Math.max(1, dpr);
      overlays.estimatedBeats.forEach(t => {
        const x = timeToX(t, duration, w);
        if (x < 0 || x > w) return;
        ctx.beginPath();
        ctx.moveTo(x, h * 0.15);
        ctx.lineTo(x, h * 0.85);
        ctx.stroke();
      });
    }
    // Measured beat ticks
    if (showBeats && overlays && overlays.measuredBeats && duration > 0) {
      ctx.strokeStyle = 'rgba(255,200,80,0.85)';
      ctx.lineWidth = Math.max(1, dpr);
      overlays.measuredBeats.forEach(t => {
        const x = timeToX(t, duration, w);
        if (x < -1 || x > w + 1) return;
        ctx.beginPath();
        ctx.moveTo(x, 0);
        ctx.lineTo(x, h);
        ctx.stroke();
      });
    }
    // Chord labels (skip colliding labels)
    if (overlays && overlays.chords && duration > 0) {
      ctx.font = `${Math.max(9, 10 * dpr)}px sans-serif`;
      let lastX = -999;
      overlays.chords.forEach(ch => {
        if (!ch.label || ch.label === 'N') return;
        const x = timeToX(ch.start_time, duration, w);
        if (x < 0 || x > w) return;
        if (x - lastX < 40 * dpr) return;
        lastX = x;
        ctx.fillStyle = 'rgba(255,160,220,0.95)';
        ctx.fillText(ch.label, x + 2 * dpr, h - 4 * dpr);
      });
    }

    if (playheadX >= 0 && playheadX <= w) {
      ctx.fillStyle = useRGB ? 'rgba(255,255,255,0.92)' : waveColor;
      ctx.fillRect(playheadX, 0, Math.max(1, dpr), h);
    }
  }

  function drawPhraseBar(canvas, sections, duration, progress) {
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const cssW = canvas.clientWidth || 600;
    const cssH = canvas.clientHeight || 18;
    const w = Math.max(1, Math.floor(cssW * dpr));
    const h = Math.max(1, Math.floor(cssH * dpr));
    if (canvas.width !== w || canvas.height !== h) {
      canvas.width = w;
      canvas.height = h;
    }
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = 'rgba(0,0,0,0.35)';
    ctx.fillRect(0, 0, w, h);
    const { start, span } = viewWindow();
    if (!sections || !sections.length || !(duration > 0)) {
      ctx.fillStyle = 'rgba(255,255,255,0.25)';
      ctx.font = `${Math.max(9, 10 * dpr)}px sans-serif`;
      ctx.fillText('No phrase sections', 6 * dpr, h * 0.7);
      return;
    }
    const fontPx = Math.max(9, Math.min(11, h * 0.62));
    ctx.font = `600 ${fontPx}px sans-serif`;
    ctx.textBaseline = 'middle';
    sections.forEach(sec => {
      const t0 = (sec.start_time || 0) / duration;
      const t1 = (sec.end_time != null ? sec.end_time : duration) / duration;
      if (t1 < start || t0 > start + span) return;
      const x0 = ((t0 - start) / span) * w;
      const x1 = ((t1 - start) / span) * w;
      const style = phraseStyle(sec.label);
      ctx.fillStyle = style.fill;
      ctx.fillRect(x0, 0, Math.max(1, x1 - x0), h);
      const bw = x1 - x0;
      if (bw > 36 * dpr) {
        ctx.fillStyle = 'rgba(0,0,0,0.72)';
        ctx.fillText(style.label, x0 + 5 * dpr, h / 2);
      }
    });
    const playheadX = progress != null ? ((progress - start) / span) * w : -1;
    if (playheadX >= 0 && playheadX <= w) {
      ctx.fillStyle = 'rgba(255,255,255,0.92)';
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
    const { start, span } = viewWindow();
    const accent = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim() || '#00e5a0';
    for (let i = 0; i < vals.length; i++) {
      const t0 = i / vals.length;
      const t1 = (i + 1) / vals.length;
      if (t1 < start || t0 > start + span) continue;
      const frac = Math.min(1, Math.max(0, vals[i] / 100));
      const bh = Math.max(1, frac * h * 0.92);
      const x = ((t0 - start) / span) * w;
      const x2 = ((t1 - start) / span) * w;
      ctx.fillStyle = progress != null && t0 <= progress ? accent : 'rgba(255,255,255,0.25)';
      ctx.fillRect(x, h - bh, Math.max(1, x2 - x - dpr * 0.25), bh);
    }
    if (progress != null) {
      const px = ((progress - start) / span) * w;
      if (px >= 0 && px <= w) {
        ctx.fillStyle = accent;
        ctx.fillRect(px, 0, Math.max(1, dpr), h);
      }
    }
  }

  // VU-style: map VU (−20..+3) to 0..1 for strip display
  function vuToFrac(vu) {
    const min = -20;
    const max = 3;
    return Math.min(1, Math.max(0, (vu - min) / (max - min)));
  }

  function drawVUStrip(canvas, vu, progress) {
    if (!canvas || !vu || !vu.values || !vu.values.length) return;
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
    const vals = vu.values;
    const { start, span } = viewWindow();
    for (let i = 0; i < vals.length; i++) {
      const t0 = i / vals.length;
      const t1 = (i + 1) / vals.length;
      if (t1 < start || t0 > start + span) continue;
      const frac = vuToFrac(vals[i]);
      const bh = Math.max(1, frac * h * 0.92);
      const x = ((t0 - start) / span) * w;
      const x2 = ((t1 - start) / span) * w;
      const hot = vals[i] >= 0;
      ctx.fillStyle = progress != null && t0 <= progress
        ? (hot ? 'rgba(255,120,80,0.95)' : 'rgba(120,200,255,0.9)')
        : (hot ? 'rgba(255,120,80,0.35)' : 'rgba(255,255,255,0.22)');
      ctx.fillRect(x, h - bh, Math.max(1, x2 - x - dpr * 0.25), bh);
    }
    // 0 VU guide
    const zeroY = h - vuToFrac(0) * h * 0.92;
    ctx.strokeStyle = 'rgba(255,255,255,0.35)';
    ctx.lineWidth = Math.max(1, dpr);
    ctx.beginPath();
    ctx.moveTo(0, zeroY);
    ctx.lineTo(w, zeroY);
    ctx.stroke();
    if (progress != null) {
      const px = ((progress - start) / span) * w;
      if (px >= 0 && px <= w) {
        ctx.fillStyle = 'rgba(120,200,255,0.9)';
        ctx.fillRect(px, 0, Math.max(1, dpr), h);
      }
    }
  }

  const VU_TAU = 0.0651;
  const VU_ZERO_DBFS = -18;

  function vuStyleStep(state, sample, dt) {
    const alpha = 1 - Math.exp(-dt / VU_TAU);
    const target = Math.abs(sample);
    return state + alpha * (target - state);
  }

  function linearAmpToVU(amp) {
    if (amp <= 1e-12) return -80;
    const dbfs = 20 * Math.log10(amp);
    return dbfs - VU_ZERO_DBFS;
  }

  function ensureVUGraph() {
    if (!active || !active.audios.mix) return;
    if (active.analyser) return;
    try {
      const Ctx = window.AudioContext || window.webkitAudioContext;
      if (!Ctx) return;
      const ctx = new Ctx();
      const src = ctx.createMediaElementSource(active.audios.mix);
      const analyser = ctx.createAnalyser();
      analyser.fftSize = 2048;
      src.connect(analyser);
      analyser.connect(ctx.destination);
      active.audioCtx = ctx;
      active.analyser = analyser;
      active.vuState = 0;
      active.vuLastTs = 0;
      active.vuTimeDomain = new Float32Array(analyser.fftSize);
    } catch (_) {
      // Autoplay / CORS / already connected — live needle stays idle
    }
  }

  function updateLiveVU(ts) {
    if (!active || !active.analyser) return;
    const last = active.vuLastTs || ts;
    const dt = Math.min(0.05, Math.max(0.001, (ts - last) / 1000));
    active.vuLastTs = ts;
    active.analyser.getFloatTimeDomainData(active.vuTimeDomain);
    let peak = 0;
    for (let i = 0; i < active.vuTimeDomain.length; i++) {
      const a = Math.abs(active.vuTimeDomain[i]);
      if (a > peak) peak = a;
    }
    active.vuState = vuStyleStep(active.vuState, peak, dt);
    const vu = linearAmpToVU(active.vuState);
    const needle = active.root.querySelector('[data-vu-needle]');
    const readout = active.root.querySelector('[data-vu-readout]');
    if (needle) {
      const frac = vuToFrac(vu);
      needle.style.height = Math.max(2, frac * 100) + '%';
      needle.classList.toggle('library-vu-hot', vu >= 0);
    }
    if (readout) {
      readout.textContent = (vu > -79 ? vu.toFixed(1) : '—') + ' VU';
    }
  }

  function vuTick(ts) {
    if (!active) return;
    updateLiveVU(ts || performance.now());
    const mix = active.audios.mix;
    if (mix && !mix.paused) {
      active.vuRaf = requestAnimationFrame(vuTick);
    } else {
      active.vuRaf = null;
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
      duration: active.durations.mix || r.rms?.duration_sec || r.vu?.duration_sec || 0,
      measuredBeats: r.beat_times_measured || [],
      estimatedBeats: r.beats_complete ? [] : (r.beat_grid_estimated || []),
      beatsComplete: !!r.beats_complete,
      sections: r.labeled_sections || [],
      chords: r.chords || [],
      showMeasuredBeats: active.showMeasuredBeats !== false,
    };
  }

  function mixProgress() {
    if (!active) return 0;
    const a = active.audios && active.audios.mix;
    const dur = (a && a.duration) || active.durations.mix || 0;
    if (!(dur > 0) || !a) return 0;
    return a.currentTime / dur;
  }

  function layoutSpectrogram() {
    if (!active) return;
    const img = active.root.querySelector('#library-spectrogram');
    if (!img) return;
    const { start, zoom } = viewWindow();
    img.style.width = (zoom * 100) + '%';
    img.style.marginLeft = (-start * zoom * 100) + '%';
  }

  function redrawPhraseBars(progress) {
    if (!active) return;
    const r = active.research || {};
    const sections = r.labeled_sections || [];
    const duration = active.durations.mix || r.rms?.duration_sec || r.vu?.duration_sec || 0;
    active.root.querySelectorAll('[data-phrase]').forEach(canvas => {
      drawPhraseBar(canvas, sections, duration, progress);
    });
  }

  function redrawAllWaves(progress) {
    if (!active) return;
    if (progress == null) progress = mixProgress();
    redrawMix(progress);
    Object.keys(active.canvases || {}).forEach(name => {
      if (name === 'mix' || name === 'energy' || name === 'vu') return;
      const canvas = active.canvases[name];
      if (canvas && active.peakData[name]) {
        const a = active.audios[name];
        const dur = (a && a.duration) || active.durations[name] || 0;
        const t = a && dur > 0 ? a.currentTime / dur : 0;
        drawPeaks(canvas, active.peakData[name], active.current === name ? t : 0, null);
      }
    });
    redrawPhraseBars(progress);
    layoutSpectrogram();
  }

  function syncScrollBar() {
    if (!active || !active.root) return;
    const bar = active.root.querySelector('[data-wave-scroll]');
    if (!bar) return;
    const { start, span, zoom } = viewWindow();
    const spacer = bar.querySelector('[data-wave-spacer]');
    if (spacer) spacer.style.width = (zoom * 100) + '%';
    bar.classList.toggle('is-disabled', zoom <= 1);
    const max = bar.scrollWidth - bar.clientWidth;
    if (active._syncingScroll) return;
    active._syncingScroll = true;
    if (zoom <= 1 || max <= 0) bar.scrollLeft = 0;
    else bar.scrollLeft = (start / (1 - span)) * max;
    active._syncingScroll = false;
  }

  function setZoom(zoom, opts) {
    if (!active) return;
    const z = ZOOM_LEVELS.indexOf(zoom) >= 0 ? zoom : 1;
    const prev = viewWindow();
    const center = prev.start + prev.span / 2;
    active.zoom = z;
    const span = 1 / z;
    if (opts && opts.reset) active.viewStart = 0;
    else active.viewStart = Math.max(0, Math.min(1 - span, center - span / 2));
    syncScrollBar();
    const root = active.root;
    root.querySelectorAll('[data-zoom]').forEach(btn => {
      btn.classList.toggle('is-active', Number(btn.dataset.zoom) === z);
    });
    redrawAllWaves();
  }

  function followPlayhead(progress) {
    if (!active || (active.zoom || 1) <= 1) return;
    const { start, span } = viewWindow();
    const margin = span * 0.12;
    if (progress >= start + margin && progress <= start + span - margin) return;
    active.viewStart = Math.max(0, Math.min(1 - span, progress - span / 2));
    syncScrollBar();
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
    const vuCanvas = active.root.querySelector('[data-canvas="vu"]');
    if (vuCanvas && active.research && active.research.vu) {
      drawVUStrip(vuCanvas, active.research.vu, progress);
    }
  }

  function tick() {
    if (!active || !active.current) return;
    const a = active.audios[active.current];
    if (!a) return;
    const dur = a.duration || active.durations[active.current] || 0;
    const t = a.currentTime || 0;
    const prog = dur > 0 ? t / dur : 0;
    if (active.current === 'mix') followPlayhead(prog);
    redrawAllWaves(active.current === 'mix' ? prog : mixProgress());
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
    const el = e && e.currentTarget ? e.currentTarget : (active.canvases[name] || active.root.querySelector(`[data-canvas="${name}"]`));
    const a = active.audios.mix;
    if (!el || !a) return;
    const rect = el.getBoundingClientRect();
    const { start, span } = viewWindow();
    const ratio = start + Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width)) * span;
    const dur = a.duration || active.durations.mix || 0;
    if (dur > 0) {
      a.currentTime = ratio * dur;
      active.current = 'mix';
      redrawAllWaves(ratio);
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
    redrawAllWaves(a.currentTime / dur);
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
    if (name === 'mix') {
      ensureVUGraph();
      if (active.audioCtx && active.audioCtx.state === 'suspended') {
        active.audioCtx.resume().catch(() => {});
      }
    }
    a.play().then(() => {
      setPlaying(name, true);
      if (active.raf) cancelAnimationFrame(active.raf);
      active.raf = requestAnimationFrame(tick);
      if (name === 'mix' && active.analyser) {
        if (active.vuRaf) cancelAnimationFrame(active.vuRaf);
        active.vuRaf = requestAnimationFrame(vuTick);
      }
    }).catch(() => {
      setPlaying(name, false);
    });
  }

  function applyResearch(research) {
    if (!active) return;
    active.research = research || {};
    redrawAllWaves(0);

    const energyWrap = active.root.querySelector('#library-energy-wrap');
    if (energyWrap) {
      if (research && research.energy && research.energy.status === 'measured') {
        energyWrap.style.display = '';
        drawEnergy(active.root.querySelector('[data-canvas="energy"]'), research.energy, 0);
      } else {
        energyWrap.style.display = 'none';
      }
    }

    const vuWrap = active.root.querySelector('#library-vu-wrap');
    if (vuWrap) {
      const ok = research && research.vu && (research.vu.status === 'estimated' || research.vu.status === 'measured');
      if (ok) {
        vuWrap.style.display = '';
        drawVUStrip(active.root.querySelector('[data-canvas="vu"]'), research.vu, 0);
      } else {
        vuWrap.style.display = 'none';
      }
    }

    const notesEl = active.root.querySelector('#library-notes-list');
    if (notesEl) {
      const notes = (research && research.notes) || [];
      if (notes.length === 0) {
        notesEl.innerHTML = '<p class="field-hint">No note events (run Estimate notes when MIR tools are configured).</p>';
      } else {
        const cap = 200;
        const shown = notes.slice(0, cap);
        let html = `<table class="library-notes-table"><thead><tr>
          <th>Start</th><th>End</th><th>Name</th><th>MIDI</th><th>Stem</th></tr></thead><tbody>`;
        shown.forEach(n => {
          html += `<tr>
            <td>${esc(fmtTime(n.start_time))}</td>
            <td>${esc(fmtTime(n.end_time))}</td>
            <td>${esc(n.name || '')}</td>
            <td>${esc(String(n.midi ?? ''))}</td>
            <td>${esc(n.channel_or_stem || '')}</td>
          </tr>`;
        });
        html += '</tbody></table>';
        if (notes.length > cap) {
          html += `<p class="field-hint">${notes.length - cap} more in export</p>`;
        }
        notesEl.innerHTML = html;
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
      if (research.beats_complete) parts.push(`Beats: ${(research.beat_times_measured || []).length} (complete)`);
      else if (research.beat_times_measured) parts.push(`Beats measured: ${research.beat_times_measured.length}`);
      if (research.clipping_status === 'measured') parts.push(`Clipping runs: ${(research.clipping || []).length}`);
      if (research.labeled_sections) parts.push(`Sections: ${research.labeled_sections.length}`);
      if (research.chords) parts.push(`Chords: ${research.chords.length}`);
      if (research.notes) parts.push(`Notes: ${research.notes.length}`);
      metaExtra.textContent = parts.join(' · ') || '';
    }

    const est = active.root.querySelector('[data-legend-est]');
    if (est) est.style.display = (research && research.beats_complete) ? 'none' : '';
    const chordsLeg = active.root.querySelector('[data-legend-chords]');
    if (chordsLeg) chordsLeg.style.display = (research && research.chords && research.chords.length) ? '' : 'none';
    const swatches = active.root.querySelector('#library-phrase-swatches');
    if (swatches) {
      const seen = {};
      const order = ['intro', 'verse', 'bridge', 'chorus', 'drop', 'build', 'breakdown', 'outro'];
      (research && research.labeled_sections || []).forEach(s => {
        const k = String(s.label || 'unknown').toLowerCase();
        seen[k] = true;
      });
      const keys = order.filter(k => seen[k]);
      Object.keys(seen).forEach(k => {
        if (keys.indexOf(k) < 0) keys.push(k);
      });
      swatches.innerHTML = keys.map(k => {
        const st = phraseStyle(k);
        return `<span class="library-phrase-swatch"><i style="background:${st.fill}"></i>${esc(st.label)}</span>`;
      }).join('');
    }
  }

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  const SLOT_STORAGE_KEY = 'beatportdl.librarySlots';
  const SLOT_CHEVRON = '<svg class="search-section-chevron library-slot-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><polyline points="6 9 12 15 18 9"/></svg>';

  function slotCollapsedMap() {
    try {
      const raw = localStorage.getItem(SLOT_STORAGE_KEY);
      const map = raw ? JSON.parse(raw) : {};
      return map && typeof map === 'object' ? map : {};
    } catch (_) {
      return {};
    }
  }

  function isSlotCollapsed(id) {
    return !!slotCollapsedMap()[id];
  }

  function persistSlotCollapsed(id, collapsed) {
    try {
      const map = slotCollapsedMap();
      if (collapsed) map[id] = true;
      else delete map[id];
      localStorage.setItem(SLOT_STORAGE_KEY, JSON.stringify(map));
    } catch (_) {}
  }

  function slotOuterClass(id, extra) {
    return `library-slot ${extra || ''}${isSlotCollapsed(id) ? ' is-collapsed' : ''}`.trim();
  }

  function slotToggleHTML(id, label) {
    return `<button type="button" class="library-slot-toggle" data-slot-toggle="${id}" aria-expanded="${!isSlotCollapsed(id)}">
      ${SLOT_CHEVRON}
      <span class="analysis-metric-label">${label}</span>
    </button>`;
  }

  function redrawSlot(slotId) {
    if (!active) return;
    if (slotId === 'mix' || slotId === 'energy' || slotId === 'vu') {
      const a = active.audios && active.audios.mix;
      const dur = (a && a.duration) || active.durations.mix || 0;
      const t = a ? a.currentTime : 0;
      redrawMix(dur > 0 ? t / dur : 0);
    }
    if (slotId === 'stems') {
      Object.keys(active.canvases || {}).forEach(name => {
        if (name === 'mix' || name === 'energy' || name === 'vu') return;
        const canvas = active.canvases[name];
        if (canvas && active.peakData[name]) drawPeaks(canvas, active.peakData[name], 0, null);
      });
    }
  }

  function bindSlotToggles(playerRoot) {
    playerRoot.querySelectorAll('[data-slot-toggle]').forEach(btn => {
      btn.addEventListener('click', e => {
        e.stopPropagation();
        const id = btn.dataset.slotToggle;
        const slot = playerRoot.querySelector(`[data-slot="${id}"]`);
        if (!slot) return;
        const collapsed = !slot.classList.contains('is-collapsed');
        slot.classList.toggle('is-collapsed', collapsed);
        btn.setAttribute('aria-expanded', String(!collapsed));
        persistSlotCollapsed(id, collapsed);
        if (!collapsed) {
          requestAnimationFrame(() => redrawSlot(id));
        }
      });
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
          <canvas class="library-phrase-canvas" data-phrase="${n}" data-seek-track="${n}"></canvas>
        </div>`).join('');
    }

    root.innerHTML = `
      <div class="library-player" id="library-player">
        <div class="${slotOuterClass('mix', 'library-wave-row library-wave-mix')}" data-slot="mix" data-track="mix">
          <div class="library-wave-meta">
            ${slotToggleHTML('mix', 'Mix')}
            <button type="button" class="btn-secondary library-play-btn" data-play="mix">Play</button>
            <span class="library-wave-time" data-time="mix">0:00 / 0:00</span>
            <div class="library-vu-live" title="VU-style · 0 = −18 dBFS · not IEC 60268-17">
              <div class="library-vu-meter"><div class="library-vu-needle" data-vu-needle></div></div>
              <span class="library-vu-readout" data-vu-readout>— VU</span>
            </div>
            <div class="library-zoom" role="group" aria-label="Waveform zoom">
              <span class="library-zoom-label">Zoom</span>
              <button type="button" class="library-zoom-btn is-active" data-zoom="1">1×</button>
              <button type="button" class="library-zoom-btn" data-zoom="2">2×</button>
              <button type="button" class="library-zoom-btn" data-zoom="4">4×</button>
              <button type="button" class="library-zoom-btn" data-zoom="8">8×</button>
              <button type="button" class="library-zoom-btn" data-zoom="16">16×</button>
            </div>
          </div>
          <div class="library-slot-body">
            <p class="field-hint library-vu-caption">VU-style · 0 = −18 dBFS · not IEC 60268-17</p>
            <div class="library-wave-status" id="library-wave-status">Loading waveform…</div>
            <canvas class="library-wave-canvas" data-canvas="mix" style="display:none"></canvas>
            <canvas class="library-phrase-canvas" data-phrase="mix" data-seek-track="mix" style="display:none"></canvas>
            <div class="library-hscroll is-disabled" data-wave-scroll>
              <div class="library-hscroll-spacer" data-wave-spacer></div>
            </div>
            <div class="library-phrase-swatches" id="library-phrase-swatches"></div>
            <p class="field-hint library-overlay-legend">
              <button type="button" class="library-legend-toggle is-on" data-overlay-beats aria-pressed="true">Yellow ticks = measured beats</button>
              <span data-legend-est> · faint ticks = estimated grid</span>
              · RGB waveform: red bass · green mids · blue highs
              <span data-legend-chords style="display:none"> · pink = chord labels</span>
            </p>
          </div>
        </div>
        <div class="${slotOuterClass('stems', 'library-stems-block')}" data-slot="stems">
          <div class="library-wave-meta">${slotToggleHTML('stems', 'Stems')}</div>
          <div class="library-slot-body">${stemsHTML}</div>
        </div>
        <div class="${slotOuterClass('energy', 'library-wave-row')}" id="library-energy-wrap" data-slot="energy" style="display:none">
          <div class="library-wave-meta">${slotToggleHTML('energy', 'Energy (within-track RMS %ile)')}</div>
          <div class="library-slot-body">
            <canvas class="library-wave-canvas library-energy-canvas" data-canvas="energy"></canvas>
          </div>
        </div>
        <div class="${slotOuterClass('vu', 'library-wave-row')}" id="library-vu-wrap" data-slot="vu" style="display:none">
          <div class="library-wave-meta">${slotToggleHTML('vu', 'VU-style timeline (not IEC 60268-17)')}</div>
          <div class="library-slot-body">
            <canvas class="library-wave-canvas library-energy-canvas" data-canvas="vu"></canvas>
          </div>
        </div>
        <div class="${slotOuterClass('spectrogram', 'library-spectrogram-wrap')}" data-slot="spectrogram">
          <div class="library-wave-meta">${slotToggleHTML('spectrogram', 'Spectrogram')}</div>
          <div class="library-slot-body">
            <div class="library-spectrogram-status" id="library-spec-status">Loading spectrogram…</div>
            <div class="library-spec-clip">
              <img class="library-spectrogram" id="library-spectrogram" alt="Spectrogram" style="display:none" />
            </div>
          </div>
        </div>
        <p class="library-research-meta" id="library-research-meta"></p>
        <div class="${slotOuterClass('issues', 'library-issues')}" data-slot="issues">
          <div class="library-wave-meta">${slotToggleHTML('issues', 'Issues')}</div>
          <div class="library-slot-body" id="library-issues-list"><p class="field-hint">Loading research…</p></div>
        </div>
        <div class="${slotOuterClass('notes', 'library-notes-block')}" data-slot="notes">
          <div class="library-wave-meta">${slotToggleHTML('notes', 'Notes (estimated)')}</div>
          <div class="library-slot-body" id="library-notes-list"><p class="field-hint">Loading…</p></div>
        </div>
      </div>`;

    const playerRoot = root.querySelector('#library-player');
    bindSlotToggles(playerRoot);
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
      zoom: 1,
      viewStart: 0,
      showMeasuredBeats: loadShowMeasuredBeats(),
    };

    const beatsBtn = playerRoot.querySelector('[data-overlay-beats]');
    if (beatsBtn) {
      beatsBtn.classList.toggle('is-on', active.showMeasuredBeats);
      beatsBtn.setAttribute('aria-pressed', String(active.showMeasuredBeats));
      beatsBtn.addEventListener('click', e => {
        e.stopPropagation();
        active.showMeasuredBeats = !active.showMeasuredBeats;
        persistShowMeasuredBeats(active.showMeasuredBeats);
        beatsBtn.classList.toggle('is-on', active.showMeasuredBeats);
        beatsBtn.setAttribute('aria-pressed', String(active.showMeasuredBeats));
        redrawAllWaves();
      });
    }
    playerRoot.querySelectorAll('[data-zoom]').forEach(btn => {
      btn.addEventListener('click', e => {
        e.stopPropagation();
        setZoom(Number(btn.dataset.zoom));
      });
    });
    const scrollBar = playerRoot.querySelector('[data-wave-scroll]');
    if (scrollBar) {
      scrollBar.addEventListener('scroll', () => {
        if (!active || active._syncingScroll) return;
        const zoom = active.zoom || 1;
        if (zoom <= 1) {
          active.viewStart = 0;
          return;
        }
        const max = scrollBar.scrollWidth - scrollBar.clientWidth;
        const span = 1 / zoom;
        active.viewStart = max > 0 ? (scrollBar.scrollLeft / max) * (1 - span) : 0;
        redrawAllWaves();
      });
    }
    playerRoot.addEventListener('wheel', e => {
      if (!active || (active.zoom || 1) <= 1) return;
      if (!e.target.closest('[data-canvas], [data-phrase], .library-spec-clip, .library-hscroll')) return;
      if (Math.abs(e.deltaX) < Math.abs(e.deltaY) && !e.shiftKey) return;
      e.preventDefault();
      if (!scrollBar) return;
      scrollBar.scrollLeft += e.deltaX || e.deltaY;
    }, { passive: false });

    const onResize = () => {
      if (!active) return;
      syncScrollBar();
      redrawAllWaves();
    };
    window.addEventListener('resize', onResize);
    active._onResize = onResize;

    playerRoot.querySelectorAll('[data-play]').forEach(btn => {
      btn.addEventListener('click', e => {
        e.stopPropagation();
        togglePlay(btn.dataset.play);
      });
    });
    playerRoot.querySelectorAll('[data-canvas="mix"], [data-canvas="energy"], [data-canvas="vu"], [data-phrase]').forEach(canvas => {
      canvas.addEventListener('click', e => {
        e.stopPropagation();
        const track = canvas.dataset.seekTrack || canvas.dataset.canvas;
        if (track && track !== 'mix' && track !== 'energy' && track !== 'vu' && audios[track]) {
          const a = audios[track];
          const rect = canvas.getBoundingClientRect();
          const { start, span } = viewWindow();
          const ratio = start + Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width)) * span;
          const dur = a.duration || durations[track] || 0;
          if (dur > 0) {
            a.currentTime = ratio * dur;
            active.current = track;
            drawPeaks(canvases[track], peakData[track], ratio, null);
            redrawPhraseBars(mixProgress());
          }
          return;
        }
        seekFromClick('mix', e);
      });
    });
    playerRoot.querySelectorAll('[data-canvas]').forEach(canvas => {
      const name = canvas.dataset.canvas;
      if (name === 'mix' || name === 'energy' || name === 'vu') return;
      canvas.addEventListener('click', e => {
        e.stopPropagation();
        const a = audios[name];
        if (!a) return;
        const rect = canvas.getBoundingClientRect();
        const { start, span } = viewWindow();
        const ratio = start + Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width)) * span;
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
          peakData.mix = data.mix;
          if (data.mix.duration_sec) durations.mix = data.mix.duration_sec;
          mixCanvas.style.display = '';
          const phraseMix = playerRoot.querySelector('[data-phrase="mix"]');
          if (phraseMix) phraseMix.style.display = '';
          redrawAllWaves(0);
        }
        const stemPeaks = data.stems || {};
        stemNames.forEach(n => {
          const p = stemPeaks[n];
          if (!p || !p.peaks) return;
          peakData[n] = p;
          if (p.duration_sec) durations[n] = p.duration_sec;
          const c = canvases[n];
          if (c) drawPeaks(c, peakData[n], 0, null);
        });
        redrawPhraseBars(0);
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
        layoutSpectrogram();
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
