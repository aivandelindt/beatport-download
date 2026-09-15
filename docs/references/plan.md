The important distinction: **Go should manage all tasks, while specialized local workers perform the audio processing.** You still have one application, one browser interface, and one API.

## 1. Architecture I recommend

```text
Existing HTML / JavaScript UI
    │
    │ REST requests + job progress events
    ▼
Existing Go API server (Add what's missing)
    ├── Library, search, settings
    ├── Persistent job queue
    ├── Processing-worker management
    ├── Audio streaming and waveform endpoints
    └── SQLite writes
             │
             ├── Native Python separation worker
             ├── Native Python musical-analysis worker
             ├── Isolated Basic Pitch worker
             └── FFmpeg / preview-rendering processes
                         │
                         ▼
             Local audio/artifact directories
```

**Only the Go server would access SQLite directly.** Workers would return structured results and artifact paths; Go would validate and persist them.

For the PoC, I would launch workers as subprocesses rather than introduce another HTTP server. Go’s `os/exec` provides subprocess execution and context-based cancellation without requiring shell command strings. ([pkg.go.dev](https://pkg.go.dev/os/exec))

My proposed worker contract:

- **Input:** versioned JSON job specification.
- **Output:** newline-delimited JSON progress/result events.
- **Diagnostics:** stderr.
- **Artifacts:** files inside a job-specific directory.
- **Completion:** Go validates outputs before marking the job successful.

## 2. Processing tools behind your API

| Task                           | My choice                                                    | Initial scope                                                                                                                                                                                                                                  |
| ------------------------------ | ------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Stem separation**            | `audio-separator`, starting with a four-stem Demucs model    | Vocals, drums, bass, other. The wrapper documents MPS acceleration for PyTorch models and CoreML for supported ONNX models. ([github.com](https://github.com/nomadkaraoke/python-audio-separator/blob/main/README.md))                         |
| **BPM and beats**              | `librosa`                                                    | Tempo estimate and beat timestamps, with editable results. ([librosa.org](https://librosa.org/doc/0.11.0/generated/librosa.beat.beat_track.html))                                                                                              |
| **Key and chords**             | Custom harmony worker built around `librosa` chroma features | My proposed baseline: major/minor key-profile matching, chord-template matching, and temporal smoothing—not a built-in comprehensive chord detector. ([librosa.org](https://librosa.org/doc/0.11.0/generated/librosa.feature.chroma_cqt.html)) |
| **Notes and MIDI**             | Spotify Basic Pitch                                          | Start with bass and isolated lead/vocal passages. It supports polyphony but works best on one instrument at a time. ([github.com](https://github.com/spotify/basic-pitch))                                                                     |
| **Loudness and normalization** | FFmpeg                                                       | Loudness measurement and optional two-pass normalization. ([ffmpeg.org](https://ffmpeg.org/ffmpeg-filters.html?utm_source=openai))                                                                                                             |
| **Mashup previews**            | FFmpeg with Rubber Band support                              | Backend-rendered tempo/pitch changes and mixes. Verify the FFmpeg build includes `librubberband`; it is not universally enabled. ([ffmpeg.org](https://ffmpeg.org/ffmpeg-filters.html?utm_source=openai))                                      |

I would treat detailed chord extensions, inversions, and notes inside a dense “other” stem as **reviewable estimates**, not guaranteed recovered notation.

### Defaults for your M5 / 16 GB machine

I would start with:

- Native **arm64 macOS** workers.
- **One heavy inference job at a time**, shared across separation and transcription.
- One transcription stem at a time.
- A **30–60-second selected passage** for initial validation.
- Separation and Basic Pitch in separate dependency environments.
- Cached results and models; no unnecessary repeat separation.
- Worker shutdown after heavy jobs initially, rather than keeping every model resident.

At setup, verify MPS availability and run an actual separation test. MPS availability alone does not prove that every operation in a particular model will stay accelerated; the separator documents CPU fallback for unsupported spectral operations. ([docs.pytorch.org](https://docs.pytorch.org/docs/stable/notes/mps.html?utm_source=openai))

**I would not promise a processing speed until benchmarking your chosen models on that machine.**

## 3. Changes I would make to the displayed UI

Your existing layout already provides the right foundation. I would extend it rather than redesign it.

### First: distinguish missing results from measured values

The screenshot shows `AUDIO_INFO` records with `0.0` BPM and LUFS. I cannot determine from the image whether these are placeholders or stored values.

If they mean “not analyzed,” I would change the behavior to:

```text
Database/API: null
UI:           —
Status:       Not analyzed
```

I would also track separate statuses for **metadata, rhythm, harmony, stems, and notes**, rather than one overall “analyzed” flag.

### Split analysis into two actions

**Quick analysis**
- File metadata.
- Loudness.
- Waveform peaks.
- Initial tempo, beats, and key estimate.
- No mandatory stem separation.

**Deep analysis**
- Reuse or generate stems.
- Refine rhythm using the drum stem.
- Analyze harmony using suitable instrumental material.
- Transcribe selected bass/lead passages.
- Export note events and MIDI.

That is my proposed pipeline—not a requirement that every action run every tool.

### Extend the expanded track panel

Alongside the existing waveform players, I would add:

```text
Overview | Beats | Chords | Notes | Stems | Compare
```

With:

- Editable BPM and half/double-tempo controls.
- Editable first-downbeat position.
- Chord labels aligned to the waveform.
- Note piano roll with stem selection.
- Region selection and loop audition.
- Model/version and analysis-status details.
- “Compare with another track.”
- “Render preview.”

Store automatic estimates separately from manual corrections so re-analysis does not erase your edits.

## 4. API and job design

I would make processing asynchronous and persistent.

An example API shape:

```http
POST /api/tracks/import

POST /api/tracks/{id}/jobs
GET  /api/jobs/{id}
GET  /api/jobs/{id}/events
POST /api/jobs/{id}/cancel

GET  /api/tracks/{id}/analysis
GET  /api/tracks/{id}/artifacts

GET  /api/artifacts/{id}/audio
GET  /api/artifacts/{id}/peaks

POST /api/mashup-previews
```

For example, a proposed deep-analysis request:

```json
{
  "type": "deep_analysis",
  "region": {
    "start_seconds": 60,
    "end_seconds": 120
  },
  "tasks": ["stems", "beats", "key", "chords", "notes"],
  "note_sources": ["bass", "vocals"],
  "reuse_cached": true
}
```

Return a job ID immediately. The UI then receives stage updates:

```text
Queued → Preparing → Separating → Analyzing → Saving → Complete
```

I would also implement cancellation, per-stage errors, and recovery of interrupted jobs after a server restart.

## 5. SQLite and artifact storage

**Keep searchable results in SQLite; keep large audio files on disk.**

My proposed entities:

| Entity            | Contents                                          |
| ----------------- | ------------------------------------------------- |
| `tracks`          | Source identity, content hash, metadata           |
| `jobs`            | Requested work, stage, state, errors              |
| `analysis_runs`   | Tool/model versions, settings, input artifact     |
| `artifacts`       | Stems, MIDI, previews, waveform files             |
| `beats`           | Beat times and reviewed downbeat markers          |
| `chords`          | Start/end times, labels, optional scores          |
| `notes`           | Start/end times, MIDI pitch, source stem          |
| `overrides`       | User corrections                                  |
| `mashup_projects` | Selected regions, offsets, gains, transformations |

I would use **WAL mode**, short write transactions, and serialized writes through Go. WAL permits readers alongside a writer, but SQLite still allows only one writer at a time. ([sqlite.org](https://www.sqlite.org/wal.html?utm_source=openai))

For caching, I would include:

```text
input content hash
+ selected region
+ task
+ model/version
+ processing settings
```

All timestamps should map back to the **original track timeline**, including results generated from cropped passages.

## 6. Playback: avoid loading every full stem into browser memory

Keep your existing player if it already works. I would add backend-generated waveform peaks and stream audio on demand.

WaveSurfer’s documentation specifically recommends precomputed peaks for large files because full browser-side decoding can run into memory limits. Go’s `http.ServeContent` supports HTTP range requests for media delivery. ([wavesurfer.xyz](https://wavesurfer.xyz/docs?utm_source=openai))

For the initial mashup feature, I would **render one synchronized preview on the backend**, rather than use several independent Play buttons as the mixing engine.

I would also preserve original audio and unnormalized stems, making normalization a separate derived export.

**My next implementation milestone would be: select a region → run a persistent job → display editable beats/chords/notes → render a two-track preview.**

To translate this into changes to your existing application, the useful next inputs are the **Go routes/job code, SQLite schema, and actual HTML/JavaScript files**. The screenshot establishes the layout, but not the current implementation.