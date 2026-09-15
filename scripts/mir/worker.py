#!/usr/bin/env python3
"""Local MIR worker for BeatportDL-UI: chords (librosa chroma) and notes (Basic Pitch).

Invoked by Go as: python worker.py --health | --job <job_request.json>
Never uploads audio. Outputs JSON on stdout; diagnostics on stderr.
"""
from __future__ import annotations

import argparse
import contextlib
import json
import sys
from pathlib import Path


def health() -> dict:
    out = {
        "ok": False,
        "librosa": False,
        "basic_pitch": False,
    }
    try:
        import librosa  # noqa: F401

        out["librosa"] = True
        out["librosa_version"] = getattr(librosa, "__version__", "")
    except Exception as e:  # noqa: BLE001
        out["librosa_error"] = str(e)
    try:
        import basic_pitch  # noqa: F401

        out["basic_pitch"] = True
        out["basic_pitch_version"] = getattr(basic_pitch, "__version__", "")
    except Exception as e:  # noqa: BLE001
        out["basic_pitch_error"] = str(e)
    out["ok"] = out["librosa"] or out["basic_pitch"]
    return out


NOTE_NAMES = ["C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"]


def midi_to_name(midi: int) -> str:
    return f"{NOTE_NAMES[midi % 12]}{midi // 12 - 1}"


def _as_float(value) -> float | None:
    if value is None or isinstance(value, (bool, list, tuple, dict)):
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def parse_note_event(ev) -> dict | None:
    """Parse a Basic Pitch note event tuple.

    Current basic-pitch format is
    (start_time_s, end_time_s, pitch_midi, amplitude, Optional[List[int]] pitch_bends).
    Older code treated a 5th numeric field as confidence.
    """
    if ev is None or len(ev) < 4:
        return None
    start = _as_float(ev[0])
    end = _as_float(ev[1])
    pitch_f = _as_float(ev[2])
    amplitude = _as_float(ev[3])
    if start is None or end is None or pitch_f is None or amplitude is None:
        return None
    pitch = int(pitch_f)
    confidence = amplitude
    if len(ev) > 4:
        extra = _as_float(ev[4])
        if extra is not None:
            confidence = extra
    return {
        "start_time": start,
        "end_time": end,
        "midi": pitch,
        "name": midi_to_name(pitch),
        "velocity": amplitude,
        "confidence": confidence,
    }


def major_minor_templates():
    """12 major + 12 minor + N (zeros)."""
    import numpy as np

    templates = []
    labels = []
    # Major: root, major third, fifth
    for root in range(12):
        t = np.zeros(12, dtype=np.float64)
        t[root] = 1.0
        t[(root + 4) % 12] = 0.8
        t[(root + 7) % 12] = 0.9
        templates.append(t)
        labels.append(NOTE_NAMES[root])
    for root in range(12):
        t = np.zeros(12, dtype=np.float64)
        t[root] = 1.0
        t[(root + 3) % 12] = 0.8
        t[(root + 7) % 12] = 0.9
        templates.append(t)
        labels.append(NOTE_NAMES[root] + "m")
    templates.append(np.zeros(12, dtype=np.float64))
    labels.append("N")
    return templates, labels


def detect_chords(audio_path: str, stem: str) -> list[dict]:
    import librosa
    import numpy as np

    y, sr = librosa.load(audio_path, mono=True, sr=22050)
    if y.size == 0:
        return []
    hop = 2048
    chroma = librosa.feature.chroma_cqt(y=y, sr=sr, hop_length=hop)
    templates, labels = major_minor_templates()
    T = np.stack(templates, axis=0)  # (25, 12)
    # Normalize templates
    norms = np.linalg.norm(T, axis=1, keepdims=True)
    norms[norms < 1e-9] = 1.0
    T = T / norms

    frames = chroma.shape[1]
    frame_labels = []
    frame_scores = []
    for i in range(frames):
        v = chroma[:, i].astype(np.float64)
        n = np.linalg.norm(v)
        if n < 1e-6:
            frame_labels.append("N")
            frame_scores.append(0.0)
            continue
        v = v / n
        # N (silence/noise): low energy → prefer N
        rms = float(np.sqrt(np.mean(v**2)))
        scores = T @ v
        # Boost N when chroma is flat/weak
        scores[-1] = 0.15 if rms < 0.05 else scores[-1]
        idx = int(np.argmax(scores))
        frame_labels.append(labels[idx])
        frame_scores.append(float(scores[idx]))

    # Temporal smoothing: majority vote in a 5-frame window
    smoothed = list(frame_labels)
    win = 5
    for i in range(frames):
        a = max(0, i - win // 2)
        b = min(frames, i + win // 2 + 1)
        window = frame_labels[a:b]
        # most common
        best = max(set(window), key=window.count)
        smoothed[i] = best

    times = librosa.frames_to_time(np.arange(frames), sr=sr, hop_length=hop)
    segments: list[dict] = []
    if frames == 0:
        return segments
    cur = smoothed[0]
    start_i = 0
    score_acc = frame_scores[0]
    count = 1
    for i in range(1, frames):
        if smoothed[i] == cur:
            score_acc += frame_scores[i]
            count += 1
            continue
        end_t = float(times[i])
        start_t = float(times[start_i])
        if end_t - start_t >= 0.15:  # drop tiny blips
            segments.append(
                {
                    "start_time": start_t,
                    "end_time": end_t,
                    "label": cur,
                    "confidence": score_acc / max(count, 1),
                    "channel_or_stem": stem,
                    "method": "librosa chroma_cqt + major/minor templates",
                    "reliability": "estimated",
                }
            )
        cur = smoothed[i]
        start_i = i
        score_acc = frame_scores[i]
        count = 1
    end_t = float(times[-1] + (times[1] - times[0] if frames > 1 else 0.1))
    start_t = float(times[start_i])
    if end_t - start_t >= 0.15:
        segments.append(
            {
                "start_time": start_t,
                "end_time": end_t,
                "label": cur,
                "confidence": score_acc / max(count, 1),
                "channel_or_stem": stem,
                "method": "librosa chroma_cqt + major/minor templates",
                "reliability": "estimated",
            }
        )
    return segments


def detect_notes(sources: list[dict]) -> tuple[list[dict], Path | None]:
    """sources: [{path, stem}, ...] — run Basic Pitch per stem, merge."""
    from basic_pitch.inference import predict
    from basic_pitch import ICASSP_2022_MODEL_PATH

    # Prefer CoreML / ONNX over TensorFlow (macOS Python 3.10–3.11 install path).
    model_path = ICASSP_2022_MODEL_PATH
    try:
        from basic_pitch import FilenameSuffix, build_icassp_2022_model_path
        from pathlib import Path as _P

        for suffix in (FilenameSuffix.coreml, FilenameSuffix.onnx, FilenameSuffix.tflite):
            candidate = build_icassp_2022_model_path(suffix)
            # CoreML is a package dir; ONNX/tflite are files
            p = _P(candidate)
            if p.exists():
                model_path = candidate
                break
    except Exception:  # noqa: BLE001
        pass

    all_notes: list[dict] = []
    for src in sources:
        path = src["path"]
        stem = src["stem"]
        try:
            # basic-pitch prints "Predicting MIDI for ..." on stdout; keep stdout JSON-only.
            with contextlib.redirect_stdout(sys.stderr):
                _model_output, midi_data, note_events = predict(path, model_path)
        except Exception as e:  # noqa: BLE001
            print(f"basic-pitch failed on {path}: {e}", file=sys.stderr)
            continue
        for ev in note_events:
            parsed = parse_note_event(ev)
            if parsed is None:
                continue
            parsed.update(
                {
                    "channel_or_stem": stem,
                    "method": "spotify basic-pitch",
                    "reliability": "estimated",
                }
            )
            all_notes.append(parsed)
        if midi_data is not None:
            detect_notes._last_midi = midi_data  # type: ignore[attr-defined]
    all_notes.sort(key=lambda n: (n["start_time"], n["midi"]))
    return all_notes, getattr(detect_notes, "_last_midi", None)


def run_job(job: dict) -> dict:
    task = job.get("task")
    audio_input = job.get("input")
    out_dir = Path(job.get("out_dir") or ".")
    out_dir.mkdir(parents=True, exist_ok=True)
    force = bool(job.get("force"))

    if task == "chords":
        if not health().get("librosa"):
            return {"ok": False, "task": task, "error": "librosa not installed"}
        chords_path = out_dir / "chords.json"
        if chords_path.exists() and not force:
            with open(chords_path) as f:
                existing = json.load(f)
            return {"ok": True, "task": task, "out_dir": str(out_dir), "chords": existing, "source": "cache"}
        # Prefer other stem path if job provides sources
        sources = job.get("sources") or []
        path = audio_input
        stem = "mix"
        if sources:
            # Go resolves paths; worker may get list of paths via sources as roles —
            # if input is already the stem file, use stem label from sources[0]
            stem = str(sources[0]) if sources else "mix"
        if job.get("input_stem"):
            stem = job["input_stem"]
        chords = detect_chords(path, stem)
        with open(chords_path, "w") as f:
            json.dump(chords, f, indent=2)
        return {
            "ok": True,
            "task": task,
            "out_dir": str(out_dir),
            "chords": chords,
            "source": stem,
        }

    if task == "notes":
        if not health().get("basic_pitch"):
            return {"ok": False, "task": task, "error": "basic_pitch not installed"}
        notes_path = out_dir / "notes.json"
        midi_path = out_dir / "notes.mid"
        if notes_path.exists() and midi_path.exists() and not force:
            with open(notes_path) as f:
                existing = json.load(f)
            return {
                "ok": True,
                "task": task,
                "out_dir": str(out_dir),
                "notes": existing,
                "midi_path": str(midi_path),
                "source": "cache",
            }
        # sources: list of {path, stem} or we use input as mix
        raw_sources = job.get("source_files") or []
        if not raw_sources:
            raw_sources = [{"path": audio_input, "stem": "mix"}]
        notes, midi_obj = detect_notes(raw_sources)
        with open(notes_path, "w") as f:
            json.dump(notes, f, indent=2)
        if midi_obj is not None:
            try:
                midi_obj.write(str(midi_path))
            except Exception as e:  # noqa: BLE001
                print(f"midi write failed: {e}", file=sys.stderr)
        return {
            "ok": True,
            "task": task,
            "out_dir": str(out_dir),
            "notes": notes,
            "midi_path": str(midi_path) if midi_path.exists() else "",
            "source": ",".join(s.get("stem", "") for s in raw_sources),
        }

    return {"ok": False, "error": f"unknown task: {task}"}


def main() -> int:
    parser = argparse.ArgumentParser(description="BeatportDL-UI MIR worker")
    parser.add_argument("--health", action="store_true")
    parser.add_argument("--job", type=str, help="Path to job_request.json")
    args = parser.parse_args()
    if args.health:
        print(json.dumps(health()))
        return 0
    if args.job:
        with open(args.job) as f:
            job = json.load(f)
        result = run_job(job)
        print(json.dumps(result))
        return 0 if result.get("ok") else 1
    print(json.dumps({"ok": False, "error": "pass --health or --job"}), file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
