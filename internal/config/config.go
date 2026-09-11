package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Username            string `yaml:"username"            json:"username"`
	Password            string `yaml:"password"            json:"password"`
	Quality             string `yaml:"quality"             json:"quality"`
	OutputDir           string `yaml:"output_dir"          json:"output_dir"`
	MaxWorkers          int    `yaml:"max_workers"         json:"max_workers"`
	SaveCover           bool   `yaml:"save_cover"          json:"save_cover"`
	EmbedCover          bool   `yaml:"embed_cover"         json:"embed_cover"`
	StripTrackNumbers   bool   `yaml:"strip_track_numbers" json:"strip_track_numbers"`
	AutoFixMetadata     bool   `yaml:"auto_fix_metadata"   json:"auto_fix_metadata"`
	CreateSubdirs       bool   `yaml:"create_subdirs"      json:"create_subdirs"`
	Port                int    `yaml:"port"                json:"port"`
	SearchLimitArtists  int    `yaml:"search_limit_artists"  json:"search_limit_artists"`
	SearchLimitReleases int    `yaml:"search_limit_releases" json:"search_limit_releases"`
	SearchLimitLabels   int    `yaml:"search_limit_labels"   json:"search_limit_labels"`
	SearchLimitCharts   int    `yaml:"search_limit_charts"   json:"search_limit_charts"`

	AudioAnalyzerBackend string  `yaml:"audio_analyzer_backend" json:"audio_analyzer_backend"`
	AudioAnalyzerMCPPath string  `yaml:"audio_analyzer_mcp_path" json:"audio_analyzer_mcp_path"`
	AudioAnalyzerCLIPath string  `yaml:"audio_analyzer_cli_path" json:"audio_analyzer_cli_path"`
	StemSplitterPath     string  `yaml:"stem_splitter_path"     json:"stem_splitter_path"`
	StemProvider         string  `yaml:"stem_provider"          json:"stem_provider"`
	NormalizeTargetLUFS  float64 `yaml:"normalize_target_lufs"  json:"normalize_target_lufs"`
	NormalizeTruePeak    float64 `yaml:"normalize_true_peak"    json:"normalize_true_peak"`
	NormalizeLRA         float64 `yaml:"normalize_lra"          json:"normalize_lra"`
}

func DefaultConfig() *Config {
	return &Config{
		Quality:              "lossless",
		OutputDir:            defaultOutputDir(),
		MaxWorkers:           2,
		SaveCover:            true,
		EmbedCover:           true,
		StripTrackNumbers:    false,
		AutoFixMetadata:      false,
		CreateSubdirs:        true,
		Port:                 8989,
		SearchLimitArtists:   10,
		SearchLimitReleases:  10,
		SearchLimitLabels:    10,
		SearchLimitCharts:    10,
		AudioAnalyzerBackend: "auto",
		StemProvider:         "auto",
		NormalizeTargetLUFS:  -14,
		NormalizeTruePeak:    -1.5,
		NormalizeLRA:         11,
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
	return filepath.Join(Dir(), "config.yml")
}

// Dir is ~/.config/beatportdl-ui (config, credentials, pid files).
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "beatportdl-ui")
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

// ApplyDefaults fills zero/empty fields with defaults (exported for HTTP save).
func (c *Config) ApplyDefaults() {
	c.applyDefaults()
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
	if c.SearchLimitArtists <= 0 {
		c.SearchLimitArtists = 10
	}
	if c.SearchLimitReleases <= 0 {
		c.SearchLimitReleases = 10
	}
	if c.SearchLimitLabels <= 0 {
		c.SearchLimitLabels = 10
	}
	if c.SearchLimitCharts <= 0 {
		c.SearchLimitCharts = 10
	}
	if c.AudioAnalyzerBackend == "" {
		c.AudioAnalyzerBackend = "auto"
	}
	if c.StemProvider == "" {
		c.StemProvider = "auto"
	}
	// 0 cannot be chosen as a LUFS target; treat as unset.
	if c.NormalizeTargetLUFS == 0 {
		c.NormalizeTargetLUFS = -14
	}
	if c.NormalizeTruePeak == 0 {
		c.NormalizeTruePeak = -1.5
	}
	if c.NormalizeLRA == 0 {
		c.NormalizeLRA = 11
	}
}
