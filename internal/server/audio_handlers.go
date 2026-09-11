package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	if s.analysisStore == nil {
		respondErr(w, 503, "analysis store unavailable")
		return
	}
	id, err := parseLibraryID(r.PathValue("id"))
	if err != nil {
		respondErr(w, 400, "invalid id")
		return
	}
	rec, err := s.analysisStore.GetByID(r.Context(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondErr(w, 404, "not found")
		return
	}
	if err != nil {
		respondErr(w, 500, err.Error())
		return
	}
	var analysis audio.Analysis
	if rec.PayloadJSON != "" {
		if err := json.Unmarshal([]byte(rec.PayloadJSON), &analysis); err != nil {
			respondErr(w, 500, "invalid stored analysis payload")
			return
		}
	}
	respond(w, 200, map[string]interface{}{
		"id":              rec.ID,
		"path":            rec.Path,
		"kind":            rec.Kind,
		"key":             rec.Key,
		"mode":            rec.Mode,
		"tempo_bpm":       rec.TempoBPM,
		"lufs_integrated": rec.LUFSIntegrated,
		"duration_sec":    rec.DurationSec,
		"analyzed_at":     rec.AnalyzedAt,
		"source":          rec.Source,
		"analysis":        analysis,
	})
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
