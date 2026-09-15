package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"beatportdl-ui/internal/audio"
	"beatportdl-ui/internal/audio/store"

	"gorm.io/gorm"
)

func openTemp(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestUpsertAndGetFresh_Hit(t *testing.T) {
	s := openTemp(t)
	path := "/music/a.flac"
	a := audio.Analysis{
		Kind:   audio.KindFullAnalysis,
		Path:   path,
		Source: "mcp",
		HarmonicAnalysis: &audio.HarmonicAnalysis{Key: "A", Mode: "minor"},
		RhythmAnalysis:   &audio.RhythmAnalysis{TempoBPM: 128},
		SpectralFeatures: &audio.SpectralFeatures{LUFSIntegrated: -9.5},
		AudioInfo:        &audio.AudioInfo{DurationSec: 180},
	}
	rec, err := store.RecordFromAnalysis(path, 1000, 1700000000, a)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetFresh(context.Background(), path, audio.KindFullAnalysis, 1000, 1700000000)
	if err != nil || !ok {
		t.Fatalf("hit=%v err=%v", ok, err)
	}
	if got.HarmonicAnalysis == nil || got.HarmonicAnalysis.Key != "A" {
		t.Fatalf("%+v", got)
	}
}

func TestGetFresh_MissOnMtimeChange(t *testing.T) {
	s := openTemp(t)
	path := "/music/a.flac"
	a := audio.Analysis{Kind: audio.KindFullAnalysis, Path: path, Source: "mcp"}
	rec, err := store.RecordFromAnalysis(path, 1000, 1700000000, a)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	_, ok, err := s.GetFresh(context.Background(), path, audio.KindFullAnalysis, 1000, 1700000001)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected cache miss on mtime change")
	}
}

func TestGetFresh_FullSatisfiesPartial(t *testing.T) {
	s := openTemp(t)
	path := "/music/a.flac"
	a := audio.Analysis{
		Kind:             audio.KindFullAnalysis,
		Path:             path,
		Source:           "mcp",
		RhythmAnalysis:   &audio.RhythmAnalysis{TempoBPM: 128},
		HarmonicAnalysis: &audio.HarmonicAnalysis{Key: "A", Mode: "minor"},
	}
	rec, err := store.RecordFromAnalysis(path, 1000, 1700000000, a)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetFresh(context.Background(), path, audio.KindRhythmAnalysis, 1000, 1700000000)
	if err != nil || !ok {
		t.Fatalf("hit=%v err=%v", ok, err)
	}
	if got.Kind != audio.KindRhythmAnalysis {
		t.Fatalf("kind=%q", got.Kind)
	}
	if got.RhythmAnalysis == nil || got.RhythmAnalysis.TempoBPM != 128 {
		t.Fatalf("%+v", got)
	}
	if got.HarmonicAnalysis != nil {
		t.Fatal("expected harmonic trimmed from partial result")
	}
}

func TestList_FilterByPathSubstring(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	for _, tc := range []struct {
		path string
		key  string
	}{
		{"/music/foo/bar.flac", "A"},
		{"/music/baz.flac", "B"},
	} {
		a := audio.Analysis{
			Kind:             audio.KindFullAnalysis,
			Path:             tc.path,
			Source:           "mcp",
			HarmonicAnalysis: &audio.HarmonicAnalysis{Key: tc.key, Mode: "minor"},
		}
		rec, err := store.RecordFromAnalysis(tc.path, 1000, 1700000000, a)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Upsert(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := s.List(ctx, store.ListFilter{Q: "foo", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total=%d len=%d", total, len(items))
	}
	if items[0].Path != "/music/foo/bar.flac" {
		t.Fatalf("path=%q", items[0].Path)
	}
}

func TestDelete(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	path := "/music/a.flac"
	a := audio.Analysis{Kind: audio.KindFullAnalysis, Path: path, Source: "mcp"}
	rec, err := store.RecordFromAnalysis(path, 1000, 1700000000, a)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(ctx, rec); err != nil {
		t.Fatal(err)
	}
	items, _, err := s.List(ctx, store.ListFilter{Limit: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("list err=%v len=%d", err, len(items))
	}
	id := items[0].ID
	if err := s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetByID(ctx, id)
	if err == nil {
		t.Fatal("expected error after delete")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err=%v", err)
	}
}
