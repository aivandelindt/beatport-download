package audio

const (
	KindAudioInfo        = "audio_info"
	KindSpectralFeatures = "spectral_features"
	KindHarmonicAnalysis = "harmonic_analysis"
	KindRhythmAnalysis   = "rhythm_analysis"
	KindFullAnalysis     = "full_analysis"
)

const (
	BackendAuto = "auto"
	BackendMCP  = "mcp"
	BackendCLI  = "cli"
)

// EnrichVersion is bumped when post-parse enrichment logic changes.
const EnrichVersion = "1"

type Analysis struct {
	Kind             string            `json:"kind"`
	Source           string            `json:"source"` // "mcp" | "cli" | "cache"
	Path             string            `json:"path,omitempty"`
	RawText          string            `json:"raw_text,omitempty"`
	Meta             *AnalysisMeta     `json:"meta,omitempty"`
	AudioInfo        *AudioInfo        `json:"audio_info,omitempty"`
	SpectralFeatures *SpectralFeatures `json:"spectral_features,omitempty"`
	HarmonicAnalysis *HarmonicAnalysis `json:"harmonic_analysis,omitempty"`
	RhythmAnalysis   *RhythmAnalysis   `json:"rhythm_analysis,omitempty"`
	Percussive       *Percussive       `json:"percussive,omitempty"`
	Masking          *MaskingAnalysis  `json:"masking,omitempty"`
	Sections         []SectionBoundary `json:"sections,omitempty"`
	LabeledSections  []LabeledSection  `json:"labeled_sections,omitempty"`
	Issues           []Finding         `json:"issues,omitempty"`
}

type AnalysisMeta struct {
	FileSHA256    string `json:"file_sha256,omitempty"`
	EnrichVersion string `json:"enrich_version,omitempty"`
	AnalyzerKind  string `json:"analyzer_kind,omitempty"`
	AnalyzerSource string `json:"analyzer_source,omitempty"`
	FFmpegVersion string `json:"ffmpeg_version,omitempty"`
}

type AudioInfo struct {
	Path        string  `json:"path"`
	SampleRate  int     `json:"sample_rate"`
	Samples     int64   `json:"samples"`
	DurationSec float64 `json:"duration_sec"`
}

type SpectralFeatures struct {
	CentroidHz       float64            `json:"centroid_hz"`
	BandwidthHz      float64            `json:"bandwidth_hz"`
	RolloffHz        float64            `json:"rolloff_hz"`
	Flatness         float64            `json:"flatness"`
	RMSEnergy        float64            `json:"rms_energy"`
	ZeroCrossingRate float64            `json:"zero_crossing_rate"`
	MFCCs            []float64          `json:"mfccs"`
	BandEnergy       map[string]float64 `json:"band_energy"`
	SpectralContrast map[string]float64 `json:"spectral_contrast"`
	PeakDBFS         float64            `json:"peak_dbfs"`
	CrestFactorDB    float64            `json:"crest_factor_db"`
	LoudnessRangeDB  float64            `json:"loudness_range_db"`
	QuietRMSDBFS     *float64           `json:"quiet_rms_dbfs,omitempty"`
	LoudRMSDBFS      *float64           `json:"loud_rms_dbfs,omitempty"`
	LUFSIntegrated   float64            `json:"lufs_integrated"`
	TruePeakDBTP     float64            `json:"true_peak_dbtp"`
	LRA              float64            `json:"lra"`
	Stereo           *StereoField       `json:"stereo,omitempty"`
}

type StereoField struct {
	PhaseCorrAvg      float64 `json:"phase_corr_avg"`
	PhaseCorrMin      float64 `json:"phase_corr_min"`
	StereoWidthAvg    float64 `json:"stereo_width_avg"`
	Balance           float64 `json:"balance"`
	MonoCompatibility float64 `json:"mono_compatibility"`
}

type HarmonicAnalysis struct {
	Key                string       `json:"key"`
	Mode               string       `json:"mode"`
	Confidence         float64      `json:"confidence"`
	PitchClasses       []PitchClass `json:"pitch_classes"`
	Camelot            string       `json:"camelot,omitempty"`
	CamelotReliability string       `json:"camelot_reliability,omitempty"` // "estimated"
}

type PitchClass struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type RhythmAnalysis struct {
	TempoBPM           float64   `json:"tempo_bpm"`
	Confidence         float64   `json:"confidence"`
	BeatsDetected      int       `json:"beats_detected"`
	MeanTempoBPM       float64   `json:"mean_tempo_bpm"`
	MedianTempoBPM     float64   `json:"median_tempo_bpm"`
	Stability          float64   `json:"stability"`
	IBIStdSec          *float64  `json:"ibi_std_sec,omitempty"`
	BeatTimesSec       []float64 `json:"beat_times_sec,omitempty"`
	BeatTimesSource    string    `json:"beat_times_source,omitempty"` // "measured"
	BeatGridEstimated  []float64 `json:"beat_grid_estimated,omitempty"`
	TempoHalfBPM       float64   `json:"tempo_half_bpm,omitempty"`
	TempoDoubleBPM     float64   `json:"tempo_double_bpm,omitempty"`
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

// MaskingAnalysis is parsed from the analyzer's Frequency Masking text block.
type MaskingAnalysis struct {
	BandCrowding []BandCrowding   `json:"band_crowding,omitempty"`
	HPCollision  []HPCollision    `json:"hp_collision,omitempty"`
	CrossBleed   []CrossBandBleed `json:"cross_bleed,omitempty"`
}

type BandCrowding struct {
	Band  string  `json:"band"`
	Score float64 `json:"score"`
	Label string  `json:"label"` // clean | moderate | CROWDED
}

type HPCollision struct {
	Band  string  `json:"band"`
	Score float64 `json:"score"`
	Label string  `json:"label"`
}

type CrossBandBleed struct {
	Pair  string  `json:"pair"`
	Score float64 `json:"score"`
	Label string  `json:"label"`
}

// LabeledSection is a heuristic arrangement label between novelty boundaries.
type LabeledSection struct {
	StartTime   float64 `json:"start_time"`
	EndTime     float64 `json:"end_time"`
	Label       string  `json:"label"` // intro|verse|chorus|build|drop|breakdown|outro|unknown
	Evidence    string  `json:"evidence"`
	Reliability string  `json:"reliability"` // estimated | low
}

// Finding follows the audio-research skill deliverable shape.
type Finding struct {
	Category       string  `json:"category"`
	StartTime      *float64 `json:"start_time"`
	EndTime        *float64 `json:"end_time"`
	ChannelOrStem  string  `json:"channel_or_stem"`
	Measurement    string  `json:"measurement"`
	Units          string  `json:"units"`
	Method         string  `json:"method"`
	Reliability    string  `json:"reliability"` // measured | estimated
	SuggestedAction string `json:"suggested_action"`
}

type AnalyzeRequest struct {
	Path    string
	Kind    string // one of Kind*
	Backend string // auto|mcp|cli
	MCPPath string
	CLIPath string
}

// Reliability constants.
const (
	ReliabilityMeasured  = "measured"
	ReliabilityEstimated = "estimated"
	ReliabilityLow       = "low"
	ReliabilityNotPerf   = "not_performed"
)
