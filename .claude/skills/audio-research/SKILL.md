---
name: music-audio-research-and-editing
description: >
  Research, analyze, visualize, and non-destructively process music audio.
  Covers stem separation, normalization, energy, chorus and drop detection,
  note transcription, tempo/BPM, musical key and Camelot notation,
  harmonics, loudness, RMS, VU-style metering, frequency bands,
  masking, clipping, and waveform editing.
---

# Music Audio Research & Editing

## 1. Purpose

Turn music files into:

- A technical and musical analysis report.
- Timestamped findings and suggested corrections.
- Waveform, spectrum, loudness, and musical-structure visualizations.
- Optional separated stems and estimated MIDI notes.
- Optional processed audio with before/after measurements.

**Default mode: analyze first. Preserve the original. Apply edits only when requested or approved.**

## 2. Inputs and operating rules

Accept one track, multiple tracks, or existing stems.

Collect, when relevant:

- Audio files and optional reference track.
- Intended use: research, mixing, mastering, DJ preparation, or transcription.
- Requested processing and export format.
- Loudness target and true-peak ceiling.
- Whether tempo, pitch, or arrangement changes are permitted.

If no processing targets are supplied, produce an analysis and proposed edit plan rather than silently choosing mastering settings.

Always:

1. Preserve the original file.
2. Record file hash, metadata, tool/model versions, and parameters.
3. Keep native-channel audio for level and stereo analysis; make separate analysis copies when algorithms require resampling or mono.
4. Record all resampling, gain changes, and time offsets.
5. Request permission before uploading audio to external services.
6. Report unavailable capabilities rather than inventing results.

## 3. Research and tool selection

Before installation, verify compatibility, maintenance status, licenses, model requirements, and CPU/GPU support using official documentation.

Suggested components:

| Component | Role |
|---|---|
| FFmpeg / ffprobe | Decode, inspect, meter, filter, and export audio |
| librosa | RMS, chroma, spectral, onset, and musical features |
| Essentia | Tempo and musical-key estimation |
| Demucs-compatible separator | Vocal, drum, bass, and other-stem extraction |
| Spotify Basic Pitch | Estimated note events and MIDI |
| Plotting/UI layer | Interactive analysis and edit previews |

FFmpeg documents loudness measurement and normalization filters; librosa documents RMS and chroma features; Essentia exposes tempo and key estimators. Basic Pitch supports audio-to-MIDI transcription and works best with one instrument at a time. The original Demucs repository is archived, so verify a suitable implementation before deployment. ([ffmpeg.org](https://ffmpeg.org/ffmpeg-filters.html?utm_source=openai))

## 4. Analysis workflow

### A. File inspection

Record:

- Container, codec, duration, sample rate, channels, and available bit-depth information.
- Decode errors, silence intervals, and DC offset.
- Sample peaks and RMS per channel.
- Original metadata and analysis configuration.

Do not infer an original recording bit depth from a lossy file.

### B. Stem separation

When requested, extract:

- Vocals.
- Drums.
- Bass.
- Other instruments.
- Additional instruments only when supported by the selected model.

Preserve time alignment and document stem gain changes. Check bleed, transient damage, and reconstruction residual when stems are summed.

Demucs supports four-stem separation, but its default clipping-prevention rescaling can change relative stem levels. Do not independently normalize stems intended for faithful recombination without explicit approval. ([github.com](https://github.com/facebookresearch/demucs?utm_source=openai))

### C. Tempo and BPM

Produce:

- Main BPM estimate.
- Alternative half-time/double-time interpretations.
- Beat timestamps.
- Local tempo curve where supported.
- Confidence or explicitly labeled heuristic reliability.

Check the beat grid against multiple sections rather than relying on one global number. Follow each algorithm’s input requirements; for example, Essentia’s `RhythmExtractor2013` requires 44.1 kHz input and returns BPM, beat positions, and method-dependent confidence. ([essentia.upf.edu](https://essentia.upf.edu/reference/std_RhythmExtractor2013.html?utm_source=openai))

### D. Key and Camelot notation

Estimate:

- Tonic and scale.
- Global key and section-level alternatives.
- Tuning offset where supported.
- Camelot equivalent for supported major/minor results.

Do not force ambiguous, modal, or atonal material into a confident major/minor label. Keep estimator strength separate from calibrated probability. Essentia’s key extractor returns key, scale, and strength. ([essentia.upf.edu](https://essentia.upf.edu/reference/std_KeyExtractor.html))

For harmonic-mixing suggestions, use the same Camelot code, adjacent numbers with the same letter, and the same number with an A/B switch as starting points—not guarantees. Camelot A denotes minor; B denotes major. ([mixedinkey.com](https://mixedinkey.com/workflows/change-energy-with-camelot-wheel/?utm_source=openai))

### E. Notes and harmonics

When requested:

- Estimate note pitch, octave, onset, offset, and pitch bends.
- Export tentative MIDI and note-event CSV.
- Prefer isolated melodic stems for transcription.
- Display chroma, pitch contours, and harmonic spectra.
- Distinguish note fundamentals from their overtones.
- Mark uncertain and overlapping notes for review.

Treat transcription as an estimate, not verified sheet music. Basic Pitch supports polyphonic transcription but recommends one instrument at a time for best results. ([github.com](https://github.com/spotify/basic-pitch))

### F. Energy, chorus, and drops

Use a documented heuristic combining:

- RMS or short-term loudness.
- Onset density and transient activity.
- Bass-band activity.
- Spectral change.
- Repetition and arrangement changes.

Propose timestamped sections:

`intro`, `verse`, `chorus`, `build`, `drop`, `breakdown`, `outro`, or `unknown`.

For every section, include evidence and reliability. **Do not automatically call the loudest section the chorus or every loudness increase a drop.**

If displaying a 0–100 energy score, label it as a custom metric and state whether normalization is within-track or across a reference collection.

### G. Loudness, RMS, and VU

Measure:

- Integrated LUFS.
- Momentary and short-term loudness.
- Loudness range.
- Sample peak and true peak.
- Whole-track and windowed RMS.
- Crest factor.

Document meter implementation, window lengths, channel handling, and reference conventions. FFmpeg provides `ebur128`, `loudnorm`, and `astats` for relevant measurements and processing. ([ffmpeg.org](https://ffmpeg.org/ffmpeg-filters.html?utm_source=openai))

For VU, state the calibration and ballistics. Label a simplified display **VU-style**, not a standards-compliant VU meter or an interchangeable RMS reading. A traditional VU meter has a roughly 300 ms response to its calibration tone. ([shure.com](https://www.shure.com/en-US/insights/shure-tech-tip-vu-and-ppm-audio-meters-an-elementary-explanation?utm_source=openai))

### H. Frequency bands and clashes

Use these **configurable analysis bins**, not universal tonal targets:

| Band label | Range |
|---|---:|
| Sub | 20–60 Hz |
| Bass | 60–250 Hz |
| Low mids | 250–500 Hz |
| Mids | 500 Hz–2 kHz |
| Upper mids | 2–4 kHz |
| Presence | 4–6 kHz |
| Highs | 6–20 kHz |

Restrict analysis to frequencies supported by the file’s sample rate.

Assess band energy over time and, where stems exist, pairwise overlap. Flag possible kick/bass, vocal/instrument, or high-frequency masking. Spectral overlap alone should not be treated as proof of an audible problem; masking assessment needs level and musical context. ([izotope.com](https://www.izotope.com/en/learn/what-is-frequency-masking?utm_source=openai))

Separate findings into:

- **Frequency masking**
- **Possible harmonic clash**
- **Possible phase/mono-compatibility issue**
- **Clipping or overload risk**

### I. Clipping and distortion

Flag suspicious full-scale sample runs, flattened peaks, and true-peak ceiling violations separately. Include timestamps and distinguish detection evidence from confirmed audible distortion.

Do not claim that turning down an already clipped recording repairs it. Declipping attempts reconstruct damaged peaks and must be reviewed against the original. ([izotope.com](https://www.izotope.com/en/learn/how-to-fix-audio-clipping.html?utm_source=openai))

## 5. Visualizations

Create synchronized, zoomable views where the runtime supports them:

1. Stereo waveform with peak and RMS overlays.
2. Spectrogram with logarithmic frequency axis.
3. Average spectrum and selected-region spectrum.
4. LUFS, RMS, true-peak, and energy timelines.
5. Beat grid, tempo curve, and section markers.
6. Chroma, key timeline, and optional piano roll.
7. Stem waveforms and band-overlap heatmap.
8. Before/after overlays and timestamped issue markers.

For waveform editing, provide region selection, playback, loop, bypass, undo/redo, and an explicit **render/export** action. If an interactive UI is unavailable, produce static plots and an edit-decision file instead.

## 6. Adjustment workflow

For each proposed edit, specify:

- Affected file/stem and time range.
- Evidence and intended benefit.
- Processing type and parameters.
- Expected trade-offs.
- Preview and validation criteria.

Permitted operations, when requested:

- Clip gain and automation.
- Fades, trims, and crossfades.
- Peak or loudness normalization.
- EQ, dynamic EQ, and sidechain ducking.
- Compression and limiting.
- Stem balancing.
- Optional declipping.
- Explicitly authorized pitch shifting or time stretching.

For loudness normalization, prefer constant gain when feasible. If the requested loudness would exceed the peak ceiling, offer a lower target or approved dynamics processing. FFmpeg’s `loudnorm` supports linear and dynamic modes; check which mode actually ran rather than assuming normalization preserved dynamics. ([ffmpeg.org](https://ffmpeg.org/ffmpeg-filters.html?utm_source=openai))

After rendering:

- Decode and remeasure the exported file.
- Check alignment, duration, channels, loudness, and peaks.
- Compare at matched playback loudness.
- Record achieved results and unmet targets.

## 7. Deliverables

Generate only artifacts actually produced:

```text
output/
  report.md
  analysis.json
  sections.csv
  beats.csv
  issues.csv
  notes.csv                 # if transcription ran
  notes.mid                 # if transcription ran
  plots/
  stems/                    # if separation ran
  previews/                 # if edits were previewed
  processed/                # if rendering was approved
  processing_log.json
```

Every finding should include:

`category`, `start_time`, `end_time`, `channel_or_stem`, `measurement`, `units`, `method`, `reliability`, and `suggested_action`.

Use `null` for unavailable measurements. Distinguish **measured**, **estimated**, and **not performed**.

## Example invocation

> Analyze this music file for BPM, key and Camelot notation, energy, chorus/drop candidates, loudness, RMS, clipping, harmonics, and frequency masking. Separate four stems, visualize the findings, and propose corrections. Preserve the original and preview edits before rendering.