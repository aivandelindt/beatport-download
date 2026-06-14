package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Username          string `yaml:"username"           json:"username"`
	Password          string `yaml:"password"           json:"password"`
	Quality           string `yaml:"quality"            json:"quality"`
	OutputDir         string `yaml:"output_dir"         json:"output_dir"`
	MaxWorkers        int    `yaml:"max_workers"        json:"max_workers"`
	SaveCover         bool   `yaml:"save_cover"         json:"save_cover"`
	EmbedCover        bool   `yaml:"embed_cover"        json:"embed_cover"`
	StripTrackNumbers bool   `yaml:"strip_track_numbers" json:"strip_track_numbers"`
	AutoFixMetadata   bool   `yaml:"auto_fix_metadata"  json:"auto_fix_metadata"`
	CreateSubdirs     bool   `yaml:"create_subdirs"     json:"create_subdirs"`
	Port              int    `yaml:"port"               json:"port"`
}

func DefaultConfig() *Config {
	return &Config{
		Quality:           "lossless",
		OutputDir:         defaultOutputDir(),
		MaxWorkers:        2,
		SaveCover:         true,
		EmbedCover:        true,
		StripTrackNumbers: false,
		AutoFixMetadata:   false,
		CreateSubdirs:     true,
		Port:              8989,
	}
}

// defaultOutputDir returns /downloads if it exists (Docker), otherwise ~/Music/BeatportDL
func defaultOutputDir() string {
	if _, err := os.Stat("/downloads"); err == nil {
		return "/downloads"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Music", "BeatportDL")
}

func ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "beatportdl-ui", "config.yml")
}

func Load() (*Config, error) {
	cfg := DefaultConfig()
	path := ConfigPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	cfg.applyDefaults()
	return cfg, nil
}

func (c *Config) Save() error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

func (c *Config) applyDefaults() {
	if c.Quality == "" {
		c.Quality = "lossless"
	}
	if c.OutputDir == "" {
		home, _ := os.UserHomeDir()
		c.OutputDir = filepath.Join(home, "Music", "BeatportDL")
	}
	if c.MaxWorkers <= 0 {
		c.MaxWorkers = 3
	}
	if c.Port <= 0 {
		c.Port = 8989
	}
}
