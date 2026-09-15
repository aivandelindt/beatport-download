package audio_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"beatportdl-ui/internal/audio"
)

func TestMergeMIRArtifacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mix := filepath.Join(dir, "track.wav")
	if err := os.WriteFile(mix, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	mir := audio.DefaultMIRDir(mix)
	if err := os.MkdirAll(mir, 0755); err != nil {
		t.Fatal(err)
	}
	chords := []audio.ChordSegment{{
		StartTime: 0, EndTime: 1, Label: "Am", Reliability: audio.ReliabilityEstimated,
	}}
	b, _ := json.Marshal(chords)
	if err := os.WriteFile(filepath.Join(mir, audio.MIRChordsJSON), b, 0644); err != nil {
		t.Fatal(err)
	}
	notes := []audio.NoteEvent{{
		StartTime: 0.1, EndTime: 0.5, MIDI: 60, Name: "C4", Reliability: audio.ReliabilityEstimated,
	}}
	nb, _ := json.Marshal(notes)
	if err := os.WriteFile(filepath.Join(mir, audio.MIRNotesJSON), nb, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mir, audio.MIRNotesMIDI), []byte("MThd"), 0644); err != nil {
		t.Fatal(err)
	}

	r := audio.ResearchBundle{
		NotPerformed: []string{"midi_transcription", "chord_timeline", "waveform_editing"},
	}
	audio.MergeMIRArtifacts(&r, mix)
	if len(r.Chords) != 1 || r.Chords[0].Label != "Am" {
		t.Fatalf("chords: %+v", r.Chords)
	}
	if len(r.Notes) != 1 || r.Notes[0].MIDI != 60 {
		t.Fatalf("notes: %+v", r.Notes)
	}
	if !r.HasNotesMIDI {
		t.Fatal("expected HasNotesMIDI")
	}
	for _, n := range r.NotPerformed {
		if n == "chord_timeline" || n == "midi_transcription" {
			t.Fatalf("should drop %s from not_performed: %v", n, r.NotPerformed)
		}
	}
}

func TestDefaultMIRDir(t *testing.T) {
	t.Parallel()
	got := audio.DefaultMIRDir("/music/Foo Bar.flac")
	if got != "/music/Foo Bar_mir" {
		t.Fatalf("got %q", got)
	}
}

func TestDecodeMIRJobResultSkipsPredictingBanner(t *testing.T) {
	t.Parallel()
	stdout := []byte("Predicting MIDI for /tmp/vocals.wav...\n{\"ok\":true,\"task\":\"notes\",\"out_dir\":\"/tmp\",\"source\":\"vocals\"}\n")
	var got audio.MIRJobResult
	if err := audio.DecodeMIRJobResult(stdout, &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Task != "notes" || got.Source != "vocals" {
		t.Fatalf("decoded %+v", got)
	}
}
