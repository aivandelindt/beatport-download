package store

import "time"

type AnalysisResult struct {
	ID             uint   `gorm:"primaryKey"`
	Path           string `gorm:"uniqueIndex:idx_path_kind;size:1024;not null"`
	Kind           string `gorm:"uniqueIndex:idx_path_kind;size:64;not null"`
	FileSize       int64
	MtimeUnix      int64
	Key            string  `gorm:"index"`
	Mode           string
	TempoBPM       float64 `gorm:"index"`
	LUFSIntegrated float64 `gorm:"index"`
	DurationSec    float64
	Source         string
	PayloadJSON    string    `gorm:"type:text"`
	AnalyzedAt     time.Time `gorm:"index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (AnalysisResult) TableName() string { return "analysis_results" }
