package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"beatportdl-ui/internal/audio"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Record struct {
	ID             uint
	Path           string
	Kind           string
	FileSize       int64
	MtimeUnix      int64
	Key            string
	Mode           string
	TempoBPM       float64
	LUFSIntegrated float64
	DurationSec    float64
	Source         string
	PayloadJSON    string
	AnalyzedAt     time.Time
}

type ListItem struct {
	ID             uint     `json:"id"`
	Path           string   `json:"path"`
	Kind           string   `json:"kind"`
	Key            string   `json:"key"`
	Mode           string   `json:"mode"`
	Camelot        string   `json:"camelot,omitempty"`
	TempoBPM       *float64 `json:"tempo_bpm"`
	LUFSIntegrated *float64 `json:"lufs_integrated"`
	DurationSec    *float64 `json:"duration_sec"`
	AnalyzedAt     time.Time `json:"analyzed_at"`
	Source         string   `json:"source"`
}

type ListFilter struct {
	Q      string
	Key    string
	BPMMin float64
	BPMMax float64
	Limit  int
	Offset int
	Sort   string
}

func RecordFromAnalysis(path string, size, mtime int64, a audio.Analysis) (Record, error) {
	path, err := normalizePath(path)
	if err != nil {
		return Record{}, err
	}
	payload, err := json.Marshal(a)
	if err != nil {
		return Record{}, fmt.Errorf("marshal analysis: %w", err)
	}
	rec := Record{
		Path:        path,
		Kind:        a.Kind,
		FileSize:    size,
		MtimeUnix:   mtime,
		Source:      a.Source,
		PayloadJSON: string(payload),
		AnalyzedAt:  time.Now().UTC(),
	}
	if a.HarmonicAnalysis != nil {
		rec.Key = a.HarmonicAnalysis.Key
		rec.Mode = a.HarmonicAnalysis.Mode
	}
	if a.RhythmAnalysis != nil {
		rec.TempoBPM = a.RhythmAnalysis.TempoBPM
	}
	if a.SpectralFeatures != nil {
		rec.LUFSIntegrated = a.SpectralFeatures.LUFSIntegrated
	}
	if a.AudioInfo != nil {
		rec.DurationSec = a.AudioInfo.DurationSec
	}
	return rec, nil
}

func (s *Store) Upsert(ctx context.Context, rec Record) error {
	path, err := normalizePath(rec.Path)
	if err != nil {
		return err
	}
	row := AnalysisResult{
		Path:           path,
		Kind:           rec.Kind,
		FileSize:       rec.FileSize,
		MtimeUnix:      rec.MtimeUnix,
		Key:            rec.Key,
		Mode:           rec.Mode,
		TempoBPM:       rec.TempoBPM,
		LUFSIntegrated: rec.LUFSIntegrated,
		DurationSec:    rec.DurationSec,
		Source:         rec.Source,
		PayloadJSON:    rec.PayloadJSON,
		AnalyzedAt:     rec.AnalyzedAt,
	}
	if row.AnalyzedAt.IsZero() {
		row.AnalyzedAt = time.Now().UTC()
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "path"}, {Name: "kind"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"file_size", "mtime_unix", "key", "mode", "tempo_bpm",
			"lufs_integrated", "duration_sec", "source", "payload_json",
			"analyzed_at", "updated_at",
		}),
	}).Create(&row).Error
}

func (s *Store) GetFresh(ctx context.Context, path, kind string, size, mtime int64) (audio.Analysis, bool, error) {
	path, err := normalizePath(path)
	if err != nil {
		return audio.Analysis{}, false, err
	}
	var rows []AnalysisResult
	err = s.db.WithContext(ctx).
		Where("path = ? AND file_size = ? AND mtime_unix = ?", path, size, mtime).
		Where("kind = ? OR kind = ?", kind, audio.KindFullAnalysis).
		Find(&rows).Error
	if err != nil {
		return audio.Analysis{}, false, err
	}
	if len(rows) == 0 {
		return audio.Analysis{}, false, nil
	}
	row := pickFreshRow(rows, kind)
	if row == nil {
		return audio.Analysis{}, false, nil
	}
	var a audio.Analysis
	if err := json.Unmarshal([]byte(row.PayloadJSON), &a); err != nil {
		return audio.Analysis{}, false, err
	}
	if row.Kind == audio.KindFullAnalysis && kind != audio.KindFullAnalysis {
		a = audio.TrimAnalysis(a, kind)
	}
	return a, true, nil
}

func pickFreshRow(rows []AnalysisResult, kind string) *AnalysisResult {
	for i := range rows {
		if rows[i].Kind == kind {
			return &rows[i]
		}
	}
	for i := range rows {
		if rows[i].Kind == audio.KindFullAnalysis {
			return &rows[i]
		}
	}
	return nil
}

func (s *Store) List(ctx context.Context, f ListFilter) ([]ListItem, int64, error) {
	q := s.db.WithContext(ctx).Model(&AnalysisResult{})
	if f.Q != "" {
		q = q.Where("path LIKE ?", "%"+f.Q+"%")
	}
	if f.Key != "" {
		q = q.Where("key = ?", f.Key)
	}
	if f.BPMMin > 0 {
		q = q.Where("tempo_bpm >= ?", f.BPMMin)
	}
	if f.BPMMax > 0 {
		q = q.Where("tempo_bpm <= ?", f.BPMMax)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	sort := allowedListSort(f.Sort)
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	var rows []AnalysisResult
	if err := q.Order(sort).Limit(limit).Offset(f.Offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ListItem, len(rows))
	for i, row := range rows {
		items[i] = listItemFromRow(row)
	}
	return items, total, nil
}

func (s *Store) GetByID(ctx context.Context, id uint) (Record, error) {
	var row AnalysisResult
	err := s.db.WithContext(ctx).First(&row, id).Error
	if err != nil {
		return Record{}, err
	}
	return recordFromRow(row), nil
}

func (s *Store) Delete(ctx context.Context, id uint) error {
	res := s.db.WithContext(ctx).Delete(&AnalysisResult{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func normalizePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("normalize path: %w", err)
	}
	return filepath.Clean(abs), nil
}

func recordFromRow(row AnalysisResult) Record {
	return Record{
		ID:             row.ID,
		Path:           row.Path,
		Kind:           row.Kind,
		FileSize:       row.FileSize,
		MtimeUnix:      row.MtimeUnix,
		Key:            row.Key,
		Mode:           row.Mode,
		TempoBPM:       row.TempoBPM,
		LUFSIntegrated: row.LUFSIntegrated,
		DurationSec:    row.DurationSec,
		Source:         row.Source,
		PayloadJSON:    row.PayloadJSON,
		AnalyzedAt:     row.AnalyzedAt,
	}
}

// allowedListSort returns a fixed ORDER BY expression. Only known column
// sorts are accepted so f.Sort can never be passed raw into GORM Order().
func allowedListSort(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "", "analyzed_at desc", "analyzed_at":
		return "analyzed_at desc"
	case "analyzed_at asc":
		return "analyzed_at asc"
	case "tempo_bpm desc", "tempo_bpm":
		return "tempo_bpm desc"
	case "tempo_bpm asc":
		return "tempo_bpm asc"
	case "path asc", "path":
		return "path asc"
	case "path desc":
		return "path desc"
	default:
		return "analyzed_at desc"
	}
}

func listItemFromRow(row AnalysisResult) ListItem {
	item := ListItem{
		ID:         row.ID,
		Path:       row.Path,
		Kind:       row.Kind,
		Key:        row.Key,
		Mode:       row.Mode,
		AnalyzedAt: row.AnalyzedAt,
		Source:     row.Source,
	}
	item.Camelot = audio.CamelotFromKeyMode(row.Key, row.Mode)
	switch row.Kind {
	case audio.KindFullAnalysis, audio.KindRhythmAnalysis:
		t := row.TempoBPM
		item.TempoBPM = &t
	}
	switch row.Kind {
	case audio.KindFullAnalysis, audio.KindSpectralFeatures:
		l := row.LUFSIntegrated
		item.LUFSIntegrated = &l
	}
	switch row.Kind {
	case audio.KindFullAnalysis, audio.KindAudioInfo, audio.KindSpectralFeatures:
		d := row.DurationSec
		item.DurationSec = &d
	}
	return item
}
