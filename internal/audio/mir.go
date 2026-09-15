package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MIR artifact filenames under DefaultMIRDir.
const (
	MIRChordsJSON = "chords.json"
	MIRNotesJSON  = "notes.json"
	MIRNotesMIDI  = "notes.mid"
	MIRMetaJSON   = "mir_meta.json"
)

// DefaultMIRDir is <mix_dir>/<basename>_mir.
func DefaultMIRDir(mixPath string) string {
	dir := filepath.Dir(mixPath)
	base := mixBasename(mixPath)
	return filepath.Join(dir, base+"_mir")
}

// ResolveMIRPython returns configured path or common python3/python on PATH.
func ResolveMIRPython(configured string) string {
	if configured != "" {
		if fi, err := os.Stat(configured); err == nil && !fi.IsDir() {
			return configured
		}
	}
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// ResolveMIRWorker returns configured worker script or repo-relative scripts/mir/worker.py.
func ResolveMIRWorker(configured string) string {
	if configured != "" {
		if fi, err := os.Stat(configured); err == nil && !fi.IsDir() {
			return configured
		}
	}
	candidates := []string{
		"scripts/mir/worker.py",
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "scripts", "mir", "worker.py"),
			filepath.Join(exeDir, "..", "scripts", "mir", "worker.py"),
		)
	}
	// Walk up from cwd for monorepo layouts.
	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for i := 0; i < 6; i++ {
			candidates = append(candidates, filepath.Join(dir, "scripts", "mir", "worker.py"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			abs, err := filepath.Abs(c)
			if err == nil {
				return abs
			}
			return c
		}
	}
	return ""
}

// MIRHealth is the --health JSON from the worker.
type MIRHealth struct {
	OK          bool   `json:"ok"`
	Librosa     bool   `json:"librosa"`
	BasicPitch  bool   `json:"basic_pitch"`
	LibrosaVer  string `json:"librosa_version,omitempty"`
	BasicPitchVer string `json:"basic_pitch_version,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ProbeMIRHealth runs worker.py --health.
func ProbeMIRHealth(ctx context.Context, python, worker string) MIRHealth {
	out := MIRHealth{}
	if python == "" || worker == "" {
		out.Error = "mir python or worker not found"
		return out
	}
	cmd := exec.CommandContext(ctx, python, worker, "--health")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out.Error = strings.TrimSpace(stderr.String())
		if out.Error == "" {
			out.Error = err.Error()
		}
		return out
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		out.Error = "invalid health JSON: " + err.Error()
		return out
	}
	return out
}

// MIRJobRequest is passed to the Python worker as JSON on stdin / --job-file.
type MIRJobRequest struct {
	Task         string          `json:"task"` // chords | notes
	Input        string          `json:"input"`
	OutDir       string          `json:"out_dir"`
	Sources      []string        `json:"sources,omitempty"` // stem role labels
	InputStem    string          `json:"input_stem,omitempty"`
	SourceFiles  []MIRSourceFile `json:"source_files,omitempty"`
	Force        bool            `json:"force,omitempty"`
}

// MIRSourceFile is a resolved audio path for notes transcription.
type MIRSourceFile struct {
	Path string `json:"path"`
	Stem string `json:"stem"`
}

// DecodeMIRJobResult parses worker JSON, skipping leading stdout from libraries
// such as basic-pitch ("Predicting MIDI for ...").
func DecodeMIRJobResult(stdout []byte, dest *MIRJobResult) error {
	data := bytes.TrimSpace(stdout)
	idx := bytes.IndexByte(data, '{')
	if idx < 0 {
		return fmt.Errorf("no JSON object in worker stdout")
	}
	dec := json.NewDecoder(bytes.NewReader(data[idx:]))
	return dec.Decode(dest)
}

// MIRJobResult is the worker stdout JSON.
type MIRJobResult struct {
	OK       bool           `json:"ok"`
	Task     string         `json:"task"`
	OutDir   string         `json:"out_dir"`
	Chords   []ChordSegment `json:"chords,omitempty"`
	Notes    []NoteEvent    `json:"notes,omitempty"`
	MIDIPath string         `json:"midi_path,omitempty"`
	Source   string         `json:"source,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// RunMIRJob invokes the Python MIR worker.
func RunMIRJob(ctx context.Context, python, worker string, req MIRJobRequest) (MIRJobResult, error) {
	var result MIRJobResult
	if python == "" || worker == "" {
		return result, fmt.Errorf("MIR worker not configured (set mir_python_path / mir_worker_path)")
	}
	if req.Input == "" {
		return result, fmt.Errorf("input path required")
	}
	if req.OutDir == "" {
		req.OutDir = DefaultMIRDir(req.Input)
	}
	if err := os.MkdirAll(req.OutDir, 0755); err != nil {
		return result, err
	}

	jobPath := filepath.Join(req.OutDir, "job_request.json")
	jobBytes, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return result, err
	}
	if err := os.WriteFile(jobPath, jobBytes, 0600); err != nil {
		return result, err
	}

	cmd := exec.CommandContext(ctx, python, worker, "--job", jobPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	runErr := cmd.Run()
	_ = start
	if runErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return result, fmt.Errorf("mir worker: %s", msg)
	}
	if err := DecodeMIRJobResult(stdout.Bytes(), &result); err != nil {
		return result, fmt.Errorf("mir worker JSON: %w; stderr=%s", err, strings.TrimSpace(stderr.String()))
	}
	if !result.OK {
		if result.Error == "" {
			result.Error = "mir worker reported failure"
		}
		return result, fmt.Errorf("%s", result.Error)
	}
	return result, nil
}

// ChordSourcePath picks other stem, else mix.
func ChordSourcePath(mixPath string) (path, stem string) {
	if p := resolveStemFile(mixPath, "other"); p != "" {
		return p, "other"
	}
	return mixPath, "mix"
}

// NoteSourcePaths returns preferred stem paths for transcription.
func NoteSourcePaths(mixPath string, sources []string) []struct{ Path, Stem string } {
	var out []struct{ Path, Stem string }
	order := sources
	if len(order) == 0 {
		order = []string{"bass", "other", "vocals"}
	}
	seen := map[string]bool{}
	for _, s := range order {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		if s == "mix" {
			out = append(out, struct{ Path, Stem string }{mixPath, "mix"})
			continue
		}
		if p := resolveStemFile(mixPath, s); p != "" {
			out = append(out, struct{ Path, Stem string }{p, s})
		}
	}
	if len(out) == 0 {
		out = append(out, struct{ Path, Stem string }{mixPath, "mix"})
	}
	return out
}

// LoadChordsJSON reads chords.json from mir dir.
func LoadChordsJSON(mirDir string) ([]ChordSegment, error) {
	b, err := os.ReadFile(filepath.Join(mirDir, MIRChordsJSON))
	if err != nil {
		return nil, err
	}
	var chords []ChordSegment
	if err := json.Unmarshal(b, &chords); err != nil {
		return nil, err
	}
	return chords, nil
}

// LoadNotesJSON reads notes.json from mir dir.
func LoadNotesJSON(mirDir string) ([]NoteEvent, error) {
	b, err := os.ReadFile(filepath.Join(mirDir, MIRNotesJSON))
	if err != nil {
		return nil, err
	}
	var notes []NoteEvent
	if err := json.Unmarshal(b, &notes); err != nil {
		return nil, err
	}
	return notes, nil
}

// MergeMIRArtifacts loads chords/notes from sibling _mir dir into research.
func MergeMIRArtifacts(b *ResearchBundle, mixPath string) {
	if b == nil || mixPath == "" {
		return
	}
	mirDir := DefaultMIRDir(mixPath)
	notPerf := make([]string, 0, len(b.NotPerformed))
	hasChords := false
	hasNotes := false

	if chords, err := LoadChordsJSON(mirDir); err == nil && len(chords) > 0 {
		b.Chords = chords
		hasChords = true
	}
	if notes, err := LoadNotesJSON(mirDir); err == nil && len(notes) > 0 {
		b.Notes = notes
		hasNotes = true
	}
	midiPath := filepath.Join(mirDir, MIRNotesMIDI)
	if fi, err := os.Stat(midiPath); err == nil && !fi.IsDir() && fi.Size() > 0 {
		b.HasNotesMIDI = true
		hasNotes = true
	}

	for _, n := range b.NotPerformed {
		switch n {
		case "chord_timeline":
			if hasChords {
				continue
			}
		case "midi_transcription":
			if hasNotes {
				continue
			}
		}
		notPerf = append(notPerf, n)
	}
	// Ensure keys present when missing
	if !hasChords {
		found := false
		for _, n := range notPerf {
			if n == "chord_timeline" {
				found = true
				break
			}
		}
		if !found {
			notPerf = append(notPerf, "chord_timeline")
		}
	}
	if !hasNotes {
		found := false
		for _, n := range notPerf {
			if n == "midi_transcription" {
				found = true
				break
			}
		}
		if !found {
			notPerf = append(notPerf, "midi_transcription")
		}
	}
	b.NotPerformed = notPerf
}

// WriteChordsArtifact persists chords.json.
func WriteChordsArtifact(mixPath string, chords []ChordSegment) error {
	dir := DefaultMIRDir(mixPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(chords, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, MIRChordsJSON), b, 0644)
}

// WriteNotesArtifact persists notes.json.
func WriteNotesArtifact(mixPath string, notes []NoteEvent) error {
	dir := DefaultMIRDir(mixPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, MIRNotesJSON), b, 0644)
}
