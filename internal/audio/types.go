package audio

const (
	KindAudioInfo         = "audio_info"
	KindSpectralFeatures  = "spectral_features"
	KindHarmonicAnalysis  = "harmonic_analysis"
	KindRhythmAnalysis    = "rhythm_analysis"
	KindFullAnalysis      = "full_analysis"
)

const (
	BackendAuto = "auto"
	BackendMCP  = "mcp"
	BackendCLI  = "cli"
)

type Analysis struct {
	Kind             string            `json:"kind"`
	Source           string            `json:"source"` // "mcp" | "cli"
	Path             string            `json:"path,omitempty"`
	RawText          string            `json:"raw_text,omitempty"`
	AudioInfo        *AudioInfo        `json:"audio_info,omitempty"`
	SpectralFeatures *SpectralFeatures `json:"spectral_features,omitempty"`
	HarmonicAnalysis *HarmonicAnalysis `json:"harmonic_analysis,omitempty"`
	RhythmAnalysis   *RhythmAnalysis   `json:"rhythm_analysis,omitempty"`
	Percussive       *Percussive       `json:"percussive,omitempty"`
	Sections         []SectionBoundary `json:"sections,omitempty"`
}

type AudioInfo struct {
	Path        string  `json:"path"`
	SampleRate  int     `json:"sample_rate"`
	Samples     int64   `json:"samples"`
	DurationSec float64 `json:"duration_sec"`
}

type SpectralFeatures struct {
	CentroidHz         float64            `json:"centroid_hz"`
	BandwidthHz        float64            `json:"bandwidth_hz"`
	RolloffHz          float64            `json:"rolloff_hz"`
	Flatness           float64            `json:"flatness"`
	RMSEnergy          float64            `json:"rms_energy"`
	ZeroCrossingRate   float64            `json:"zero_crossing_rate"`
	MFCCs              []float64          `json:"mfccs"`
	BandEnergy         map[string]float64 `json:"band_energy"`
	SpectralContrast   map[string]float64 `json:"spectral_contrast"`
	PeakDBFS           float64            `json:"peak_dbfs"`
	CrestFactorDB      float64            `json:"crest_factor_db"`
	LoudnessRangeDB    float64            `json:"loudness_range_db"`
	LUFSIntegrated     float64            `json:"lufs_integrated"`
	TruePeakDBTP       float64            `json:"true_peak_dbtp"`
	LRA                float64            `json:"lra"`
	Stereo             *StereoField       `json:"stereo,omitempty"`
}

type StereoField struct {
	PhaseCorrAvg      float64 `json:"phase_corr_avg"`
	PhaseCorrMin      float64 `json:"phase_corr_min"`
	StereoWidthAvg    float64 `json:"stereo_width_avg"`
	Balance           float64 `json:"balance"`
	MonoCompatibility float64 `json:"mono_compatibility"`
}

type HarmonicAnalysis struct {
	Key          string       `json:"key"`
	Mode         string       `json:"mode"`
	Confidence   float64      `json:"confidence"`
	PitchClasses []PitchClass `json:"pitch_classes"`
}

type PitchClass struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type RhythmAnalysis struct {
	TempoBPM       float64   `json:"tempo_bpm"`
	Confidence     float64   `json:"confidence"`
	BeatsDetected  int       `json:"beats_detected"`
	MeanTempoBPM   float64   `json:"mean_tempo_bpm"`
	MedianTempoBPM float64   `json:"median_tempo_bpm"`
	Stability      float64   `json:"stability"`
	BeatTimesSec   []float64 `json:"beat_times_sec,omitempty"`
}

type Percussive struct {
	PercussiveRatio float64 `json:"percussive_ratio"`
	OnsetDensity    float64 `json:"onset_density"`
	PeakAttackSharp float64 `json:"peak_attack_sharp"`
}

type SectionBoundary struct {
	TimeSec    float64 `json:"time_sec"`
	Reasons    string  `json:"reasons"`
	Confidence float64 `json:"confidence"`
}

type AnalyzeRequest struct {
	Path    string
	Kind    string // one of Kind*
	Backend string // auto|mcp|cli
	MCPPath string
	CLIPath string
}
