package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"beatportdl-ui/internal/audio"
	"beatportdl-ui/internal/audio/store"
	"beatportdl-ui/internal/config"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GET /api/audio/tools
func (s *Server) handleAudioTools(w http.ResponseWriter, r *http.Request) {
	_ = r.Context()
	s.cfgMu.RLock()
	cfg := *s.cfg
	s.cfgMu.RUnlock()

	mcp, cli := audio.ResolveAnalyzerBins(cfg.AudioAnalyzerMCPPath, cfg.AudioAnalyzerCLIPath)
	stem := audio.ResolveStemSplitterBin(cfg.StemSplitterPath)
	_, ffmpegErr := exec.LookPath("ffmpeg")

	respond(w, 200, map[string]interface{}{
		"analyzer_mcp":          mcp != "",
		"analyzer_cli":          cli != "",
		"analyzer_mcp_path":     mcp,
		"analyzer_cli_path":     cli,
		"stem_splitter":         stem != "",
		"stem_splitter_path":    stem,
		"ffmpeg":                ffmpegErr == nil,
		"stem_provider_default": audio.DefaultStemProvider(),
		"analysis_store":        s.analysisStore != nil,
	})
}

// POST /api/audio/analyze
func (s *Server) handleAudioAnalyze(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Kind    string `json:"kind"`
		Backend string `json:"backend"`
		Force   bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, 400, "invalid JSON")
		return
	}

	s.cfgMu.RLock()
	cfg := *s.cfg
	s.cfgMu.RUnlock()

	path := req.Path
	if path == "" {
		path = cfg.OutputDir
	}
	kind := req.Kind
	if kind == "" {
		kind = audio.KindFullAnalysis
	}
	if !audio.ValidKind(kind) {
		respondErr(w, 400, "invalid kind")
		return
	}
	backend := req.Backend
	if backend == "" {
		backend = cfg.AudioAnalyzerBackend
	}

	files, err := audio.ListAudioFiles(r.Context(), path)
	if err != nil {
		respondErr(w, 400, err.Error())
		return
	}
	if len(files) == 0 {
		respondErr(w, 400, "no audio files found")
		return
	}

	jobID := uuid.New().String()[:8]
	job := &Job{
		ID:        jobID,
		URL:       path,
		Name:      filepath.Base(path),
		Kind:      "analyze",
		Status:    "pending",
		Total:     len(files),
		CreatedAt: time.Now(),
	}
	s.jobsMu.Lock()
	s.jobs[jobID] = job
	s.jobsMu.Unlock()
	s.broadcastJob(job)

	ctx := context.WithoutCancel(r.Context())
	go s.runAnalyzeJob(ctx, job, files, kind, backend, req.Force, &cfg)

	respond(w, 202, map[string]string{"job_id": jobID})
}

func (s *Server) runAnalyzeJob(ctx context.Context, job *Job, files []string, kind, backend string, force bool, cfg *config.Config) {
	job.Status = "running"
	s.broadcastJob(job)

	mcpBin, _ := audio.ResolveAnalyzerBins(cfg.AudioAnalyzerMCPPath, cfg.AudioAnalyzerCLIPath)
	var mcpClient *audio.MCPClient
	if (backend == audio.BackendMCP || backend == audio.BackendAuto || backend == "") && mcpBin != "" {
		c, err := audio.StartMCP(ctx, mcpBin)
		if err != nil {
			slog.Warn("mcp start failed, will use Analyze per-file", "err", err)
		} else {
			mcpClient = c
			defer mcpClient.Close()
		}
	}

	for i, file := range files {
		select {
		case <-ctx.Done():
			job.Status = "error"
			job.Message = ctx.Err().Error()
			s.broadcastJob(job)
			return
		default:
		}

		abs, err := filepath.Abs(file)
		if err != nil {
			job.Failed++
			job.Tracks = append(job.Tracks, TrackSummary{
				ID: i + 1, Title: filepath.Base(file), Status: "error", Message: err.Error(),
			})
			slog.Error("analyze failed", "file", file, "err", err)
			s.broadcastJob(job)
			continue
		}
		abs = filepath.Clean(abs)
		fi, err := os.Stat(abs)
		if err != nil {
			job.Failed++
			job.Tracks = append(job.Tracks, TrackSummary{
				ID: i + 1, Title: filepath.Base(file), Status: "error", Message: err.Error(),
			})
			slog.Error("analyze failed", "file", file, "err", err)
			s.broadcastJob(job)
			continue
		}

		if !force && s.analysisStore != nil {
			cached, ok, cacheErr := s.analysisStore.GetFresh(ctx, abs, kind, fi.Size(), fi.ModTime().Unix())
			if cacheErr != nil {
				slog.Warn("analysis cache lookup failed", "path", abs, "err", cacheErr)
			} else if ok {
				cached.Source = "cache"
				track := TrackSummary{
					ID:      i + 1,
					Title:   filepath.Base(file),
					Status:  "done",
					Message: "cached",
				}
				job.Completed++
				job.Analysis = append(job.Analysis, cached)
				job.Tracks = append(job.Tracks, track)
				s.hub.Broadcast(WSMessage{
					Type: "track_progress",
					Payload: ProgressPayload{
						JobID:      job.ID,
						TrackID:    i + 1,
						TrackTitle: filepath.Base(file),
						Status:     track.Status,
						Progress:   100,
						Message:    track.Message,
					},
				})
				s.broadcastJob(job)
				continue
			}
		}

		fileCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)

		s.hub.Broadcast(WSMessage{
			Type: "track_progress",
			Payload: ProgressPayload{
				JobID:      job.ID,
				TrackID:    i + 1,
				TrackTitle: filepath.Base(file),
				Status:     "downloading",
				Progress:   0,
				Message:    "analyzing",
			},
		})

		var result audio.Analysis
		if mcpClient != nil && backend != audio.BackendCLI {
			result, err = audio.AnalyzeWithClient(fileCtx, mcpClient, file, kind)
			if err != nil && (backend == audio.BackendAuto || backend == "") {
				result, err = audio.Analyze(fileCtx, audio.AnalyzeRequest{
					Path:    file,
					Kind:    kind,
					Backend: audio.BackendCLI,
					MCPPath: cfg.AudioAnalyzerMCPPath,
					CLIPath: cfg.AudioAnalyzerCLIPath,
				})
			}
		} else {
			result, err = audio.Analyze(fileCtx, audio.AnalyzeRequest{
				Path:    file,
				Kind:    kind,
				Backend: backend,
				MCPPath: cfg.AudioAnalyzerMCPPath,
				CLIPath: cfg.AudioAnalyzerCLIPath,
			})
		}
		cancel()

		if err == nil {
			if enrErr := audio.EnrichAnalysis(ctx, &result, abs); enrErr != nil {
				slog.Warn("analysis enrich failed", "path", abs, "err", enrErr)
			}
		}

		if err == nil && s.analysisStore != nil {
			rec, recErr := store.RecordFromAnalysis(abs, fi.Size(), fi.ModTime().Unix(), result)
			if recErr != nil {
				slog.Warn("analysis record build failed", "path", abs, "err", recErr)
			} else if upErr := s.analysisStore.Upsert(ctx, rec); upErr != nil {
				slog.Warn("analysis upsert failed", "path", abs, "err", upErr)
			}
		}

		track := TrackSummary{
			ID:     i + 1,
			Title:  filepath.Base(file),
			Status: "done",
		}
		if err != nil {
			job.Failed++
			track.Status = "error"
			track.Message = err.Error()
			slog.Error("analyze failed", "file", file, "err", err)
		} else {
			job.Completed++
			job.Analysis = append(job.Analysis, result)
		}
		job.Tracks = append(job.Tracks, track)
		s.hub.Broadcast(WSMessage{
			Type: "track_progress",
			Payload: ProgressPayload{
				JobID:      job.ID,
				TrackID:    i + 1,
				TrackTitle: filepath.Base(file),
				Status:     track.Status,
				Progress:   100,
				Message:    track.Message,
			},
		})
		s.broadcastJob(job)
	}

	if job.Failed > 0 && job.Completed == 0 {
		job.Status = "error"
		job.Message = "all analyses failed"
	} else {
		job.Status = "done"
	}
	s.broadcastJob(job)
}

// POST /api/audio/normalize
func (s *Server) handleAudioNormalize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path       string   `json:"path"`
		Overwrite  bool     `json:"overwrite"`
		TargetLUFS *float64 `json:"target_lufs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, 400, "invalid JSON")
		return
	}

	s.cfgMu.RLock()
	cfg := *s.cfg
	s.cfgMu.RUnlock()

	path := req.Path
	if path == "" {
		path = cfg.OutputDir
	}
	files, err := audio.ListAudioFiles(r.Context(), path)
	if err != nil {
		respondErr(w, 400, err.Error())
		return
	}
	if len(files) == 0 {
		respondErr(w, 400, "no audio files found")
		return
	}

	target := cfg.NormalizeTargetLUFS
	if req.TargetLUFS != nil {
		target = *req.TargetLUFS
	}

	jobID := uuid.New().String()[:8]
	job := &Job{
		ID:        jobID,
		URL:       path,
		Name:      filepath.Base(path),
		Kind:      "normalize",
		Status:    "pending",
		Total:     len(files),
		CreatedAt: time.Now(),
	}
	s.jobsMu.Lock()
	s.jobs[jobID] = job
	s.jobsMu.Unlock()
	s.broadcastJob(job)

	ctx := context.WithoutCancel(r.Context())
	go s.runNormalizeJob(ctx, job, files, req.Overwrite, target, &cfg)

	respond(w, 202, map[string]string{"job_id": jobID})
}

func (s *Server) runNormalizeJob(ctx context.Context, job *Job, files []string, overwrite bool, target float64, cfg *config.Config) {
	job.Status = "running"
	s.broadcastJob(job)

	for i, file := range files {
		fileCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		s.hub.Broadcast(WSMessage{
			Type: "track_progress",
			Payload: ProgressPayload{
				JobID: job.ID, TrackID: i + 1, TrackTitle: filepath.Base(file),
				Status: "downloading", Progress: 0, Message: "normalizing",
			},
		})
		res, err := audio.Normalize(fileCtx, audio.NormalizeRequest{
			Input:      file,
			TargetLUFS: target,
			TruePeak:   cfg.NormalizeTruePeak,
			LRA:        cfg.NormalizeLRA,
			Overwrite:  overwrite,
		})
		cancel()

		track := TrackSummary{ID: i + 1, Title: filepath.Base(file), Status: "done"}
		if err != nil {
			job.Failed++
			track.Status = "error"
			track.Message = err.Error()
		} else {
			job.Completed++
			job.filesMu.Lock()
			job.Files = append(job.Files, res.Output)
			job.filesMu.Unlock()
			track.Message = res.Output
		}
		job.Tracks = append(job.Tracks, track)
		s.broadcastJob(job)
	}

	if job.Failed > 0 && job.Completed == 0 {
		job.Status = "error"
	} else {
		job.Status = "done"
	}
	s.broadcastJob(job)
}

// POST /api/audio/stems
func (s *Server) handleAudioStems(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, 400, "invalid JSON")
		return
	}

	s.cfgMu.RLock()
	cfg := *s.cfg
	s.cfgMu.RUnlock()

	path := req.Path
	if path == "" {
		path = cfg.OutputDir
	}
	files, err := audio.ListAudioFiles(r.Context(), path)
	if err != nil {
		respondErr(w, 400, err.Error())
		return
	}
	if len(files) == 0 {
		respondErr(w, 400, "no audio files found")
		return
	}

	provider := req.Provider
	if provider == "" {
		provider = cfg.StemProvider
	}

	jobID := uuid.New().String()[:8]
	job := &Job{
		ID:        jobID,
		URL:       path,
		Name:      filepath.Base(path),
		Kind:      "stems",
		Status:    "pending",
		Total:     len(files),
		CreatedAt: time.Now(),
	}
	s.jobsMu.Lock()
	s.jobs[jobID] = job
	s.jobsMu.Unlock()
	s.broadcastJob(job)

	ctx := context.WithoutCancel(r.Context())
	go s.runStemsJob(ctx, job, files, provider, &cfg)

	respond(w, 202, map[string]string{"job_id": jobID})
}

func (s *Server) runStemsJob(ctx context.Context, job *Job, files []string, provider string, cfg *config.Config) {
	job.Status = "running"
	s.broadcastJob(job)

	for i, file := range files {
		fileCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		s.hub.Broadcast(WSMessage{
			Type: "track_progress",
			Payload: ProgressPayload{
				JobID: job.ID, TrackID: i + 1, TrackTitle: filepath.Base(file),
				Status: "downloading", Progress: 0, Message: "splitting stems",
			},
		})
		res, err := audio.SplitStems(fileCtx, audio.StemRequest{
			Input:    file,
			BinPath:  cfg.StemSplitterPath,
			Provider: provider,
		})
		cancel()

		track := TrackSummary{ID: i + 1, Title: filepath.Base(file), Status: "done"}
		if err != nil {
			job.Failed++
			track.Status = "error"
			track.Message = err.Error()
		} else {
			job.Completed++
			job.filesMu.Lock()
			job.StemFiles = append(job.StemFiles, res.Vocals, res.Drums, res.Bass, res.Other)
			job.Files = append(job.Files, res.Vocals, res.Drums, res.Bass, res.Other)
			job.filesMu.Unlock()
			track.Message = filepath.Dir(res.Vocals)
		}
		job.Tracks = append(job.Tracks, track)
		s.broadcastJob(job)
	}

	if job.Failed > 0 && job.Completed == 0 {
		job.Status = "error"
	} else {
		job.Status = "done"
	}
	s.broadcastJob(job)
}

// GET /api/audio/library
func (s *Server) handleAudioLibraryList(w http.ResponseWriter, r *http.Request) {
	if s.analysisStore == nil {
		respondErr(w, 503, "analysis store unavailable")
		return
	}
	q := r.URL.Query()
	f := store.ListFilter{
		Q:      q.Get("q"),
		Key:    q.Get("key"),
		Limit:  atoiDefault(q.Get("limit"), 50),
		Offset: atoiDefault(q.Get("offset"), 0),
	}
	if v, ok := parseOptionalFloat(q.Get("bpm_min")); ok {
		f.BPMMin = v
	}
	if v, ok := parseOptionalFloat(q.Get("bpm_max")); ok {
		f.BPMMax = v
	}
	items, total, err := s.analysisStore.List(r.Context(), f)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, 200, map[string]interface{}{
		"items": items,
		"total": total,
	})
}

// GET /api/audio/library/{id}
func (s *Server) handleAudioLibraryGet(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.loadLibraryRecord(w, r)
	if !ok {
		return
	}
	var analysis audio.Analysis
	if rec.PayloadJSON != "" {
		if err := json.Unmarshal([]byte(rec.PayloadJSON), &analysis); err != nil {
			respondErr(w, 500, "invalid stored analysis payload")
			return
		}
	}
	fileMissing := false
	if _, err := os.Stat(rec.Path); err != nil {
		fileMissing = true
	}
	out := map[string]interface{}{
		"id":           rec.ID,
		"path":         rec.Path,
		"kind":         rec.Kind,
		"key":          rec.Key,
		"mode":         rec.Mode,
		"analyzed_at":  rec.AnalyzedAt,
		"source":       rec.Source,
		"file_missing": fileMissing,
	}
	audio.EnsureCamelot(&analysis)
	out["analysis"] = analysis
	if analysis.HarmonicAnalysis != nil && analysis.HarmonicAnalysis.Camelot != "" {
		out["camelot"] = analysis.HarmonicAnalysis.Camelot
	} else if c := audio.CamelotFromKeyMode(rec.Key, rec.Mode); c != "" {
		out["camelot"] = c
	}
	if analysis.Issues != nil {
		out["issues"] = analysis.Issues
	}
	if analysis.LabeledSections != nil {
		out["labeled_sections"] = analysis.LabeledSections
	}
	// Null-safe metrics: omit zeros when the kind did not measure them.
	switch rec.Kind {
	case audio.KindFullAnalysis, audio.KindRhythmAnalysis:
		out["tempo_bpm"] = rec.TempoBPM
	default:
		out["tempo_bpm"] = nil
	}
	switch rec.Kind {
	case audio.KindFullAnalysis, audio.KindSpectralFeatures:
		out["lufs_integrated"] = rec.LUFSIntegrated
	default:
		out["lufs_integrated"] = nil
	}
	switch rec.Kind {
	case audio.KindFullAnalysis, audio.KindAudioInfo, audio.KindSpectralFeatures:
		out["duration_sec"] = rec.DurationSec
	default:
		out["duration_sec"] = nil
	}
	if stems, found := audio.DiscoverStems(rec.Path); found {
		stemMap := map[string]string{}
		if stems.Vocals != "" {
			stemMap["vocals"] = stems.Vocals
		}
		if stems.Drums != "" {
			stemMap["drums"] = stems.Drums
		}
		if stems.Bass != "" {
			stemMap["bass"] = stems.Bass
		}
		if stems.Other != "" {
			stemMap["other"] = stems.Other
		}
		out["stems"] = stemMap
	}
	respond(w, 200, out)
}

// GET /api/audio/library/{id}/research
func (s *Server) handleAudioLibraryResearch(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.loadLibraryRecord(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if _, err := os.Stat(rec.Path); err != nil {
		respondErr(w, 404, "audio file not found on disk")
		return
	}
	var analysis audio.Analysis
	if rec.PayloadJSON != "" {
		if err := json.Unmarshal([]byte(rec.PayloadJSON), &analysis); err != nil {
			respondErr(w, 500, "invalid stored analysis payload")
			return
		}
	}
	audio.EnsureCamelot(&analysis)
	bundle, err := audio.BuildResearch(ctx, analysis, rec.Path)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		respondErr(w, 500, err.Error())
		return
	}
	bundle.SpectrogramURL = fmt.Sprintf("/api/audio/library/%d/spectrogram", rec.ID)
	respond(w, 200, bundle)
}

// GET /api/audio/library/{id}/spectrogram
func (s *Server) handleAudioLibrarySpectrogram(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.loadLibraryRecord(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if _, err := os.Stat(rec.Path); err != nil {
		respondErr(w, 404, "audio file not found on disk")
		return
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		respondErr(w, 503, "ffmpeg not found (required for spectrogram)")
		return
	}
	png, err := audio.EnsureSpectrogram(ctx, config.Dir(), rec.Path, rec.FileSize, rec.MtimeUnix)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		respondErr(w, 500, err.Error())
		return
	}
	f, err := os.Open(png)
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "spectrogram.png", time.Unix(rec.MtimeUnix, 0), f)
}

// GET /api/audio/library/{id}/export
func (s *Server) handleAudioLibraryExport(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.loadLibraryRecord(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	var analysis audio.Analysis
	if rec.PayloadJSON != "" {
		if err := json.Unmarshal([]byte(rec.PayloadJSON), &analysis); err != nil {
			respondErr(w, 500, "invalid stored analysis payload")
			return
		}
	}
	audio.EnsureCamelot(&analysis)

	var research audio.ResearchBundle
	var spectrogram string
	if _, err := os.Stat(rec.Path); err == nil {
		research, err = audio.BuildResearch(ctx, analysis, rec.Path)
		if err != nil && ctx.Err() != nil {
			return
		}
		if err == nil {
			if png, pngErr := audio.EnsureSpectrogram(ctx, config.Dir(), rec.Path, rec.FileSize, rec.MtimeUnix); pngErr == nil {
				spectrogram = png
			}
		}
	} else {
		research = audio.ResearchBundle{
			Path:         rec.Path,
			Issues:       analysis.Issues,
			NotPerformed: []string{"ffmpeg_timelines", "spectrogram", "midi_transcription"},
		}
		if analysis.HarmonicAnalysis != nil {
			research.Camelot = analysis.HarmonicAnalysis.Camelot
		}
	}

	name := audio.BasenameSafe(rec.Path) + "-research.zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	if err := audio.WriteExportZip(ctx, w, analysis, research, spectrogram); err != nil {
		slog.Error("export zip failed", "err", err)
	}
}

// GET /api/audio/library/{id}/waveforms
func (s *Server) handleAudioLibraryWaveforms(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.loadLibraryRecord(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if _, err := os.Stat(rec.Path); err != nil {
		respondErr(w, 404, "audio file not found on disk")
		return
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		respondErr(w, 503, "ffmpeg not found (required for waveforms)")
		return
	}

	mixPeaks, err := audio.ComputePeaks(ctx, rec.Path, audio.DefaultPeakBuckets)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		respondErr(w, 500, err.Error())
		return
	}

	out := map[string]interface{}{
		"mix": mixPeaks,
	}
	if stems, found := audio.DiscoverStems(rec.Path); found {
		stemPeaks := map[string]audio.Peaks{}
		add := func(name, path string) {
			if path == "" {
				return
			}
			if !audio.AllowedInspectFile(rec.Path, path) {
				return
			}
			p, err := audio.ComputePeaks(ctx, path, audio.DefaultPeakBuckets)
			if err != nil {
				slog.Warn("stem peaks failed", "stem", name, "err", err)
				return
			}
			stemPeaks[name] = p
		}
		add("vocals", stems.Vocals)
		add("drums", stems.Drums)
		add("bass", stems.Bass)
		add("other", stems.Other)
		if len(stemPeaks) > 0 {
			out["stems"] = stemPeaks
		}
	}
	respond(w, 200, out)
}

// GET /api/audio/library/{id}/file
func (s *Server) handleAudioLibraryFile(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.loadLibraryRecord(w, r)
	if !ok {
		return
	}
	if !audio.AllowedInspectFile(rec.Path, rec.Path) {
		respondErr(w, 403, "file not allowed")
		return
	}
	serveInspectAudio(w, r, rec.Path)
}

// GET /api/audio/library/{id}/stems/{stem}
func (s *Server) handleAudioLibraryStem(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.loadLibraryRecord(w, r)
	if !ok {
		return
	}
	stem := strings.ToLower(r.PathValue("stem"))
	path := audio.StemPath(rec.Path, stem)
	if path == "" {
		respondErr(w, 400, "invalid stem name")
		return
	}
	if !audio.AllowedInspectFile(rec.Path, path) {
		respondErr(w, 403, "file not allowed")
		return
	}
	if _, err := os.Stat(path); err != nil {
		respondErr(w, 404, "stem file not found")
		return
	}
	serveInspectAudio(w, r, path)
}

func (s *Server) loadLibraryRecord(w http.ResponseWriter, r *http.Request) (store.Record, bool) {
	if s.analysisStore == nil {
		respondErr(w, 503, "analysis store unavailable")
		return store.Record{}, false
	}
	id, err := parseLibraryID(r.PathValue("id"))
	if err != nil {
		respondErr(w, 400, "invalid id")
		return store.Record{}, false
	}
	rec, err := s.analysisStore.GetByID(r.Context(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondErr(w, 404, "not found")
		return store.Record{}, false
	}
	if err != nil {
		respondErr(w, 500, err.Error())
		return store.Record{}, false
	}
	return rec, true
}

func serveInspectAudio(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		respondErr(w, 404, "file not found")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	if ct := audioContentType(path); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, filepath.Base(path), fi.ModTime(), f)
}

func audioContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	default:
		return ""
	}
}

// DELETE /api/audio/library/{id}
func (s *Server) handleAudioLibraryDelete(w http.ResponseWriter, r *http.Request) {
	if s.analysisStore == nil {
		respondErr(w, 503, "analysis store unavailable")
		return
	}
	id, err := parseLibraryID(r.PathValue("id"))
	if err != nil {
		respondErr(w, 400, "invalid id")
		return
	}
	err = s.analysisStore.Delete(r.Context(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondErr(w, 404, "not found")
		return
	}
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	respond(w, 200, map[string]string{"status": "deleted"})
}

func parseLibraryID(s string) (uint, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n == 0 {
		return 0, err
	}
	return uint(n), nil
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func parseOptionalFloat(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
