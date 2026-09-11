package store

import (
	"fmt"
	"path/filepath"
	"strings"

	sqlite "github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Store struct {
	db *gorm.DB
}

func Open(driver, dsn string) (*Store, error) {
	var dialector gorm.Dialector
	switch strings.ToLower(driver) {
	case "", "sqlite":
		if dsn == "" {
			return nil, fmt.Errorf("sqlite dsn required")
		}
		dialector = sqlite.Open(dsn)
	case "postgres", "postgresql":
		if dsn == "" {
			return nil, fmt.Errorf("postgres dsn required")
		}
		dialector = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("unknown analysis_db_driver: %s", driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&AnalysisResult{}); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func DefaultSQLiteDSN(configDir string) string {
	return filepath.Join(configDir, "analysis.db")
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
