package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"

	"beatportdl-ui/internal/audio"
	"beatportdl-ui/internal/config"

	"github.com/google/uuid"
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
	})
}

// POST /api/audio/analyze
func (s *Server) handleAudioAnalyze(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Kind    string `json:"kind"`
		Backend string `json:"backend"`
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
	go s.runAnalyzeJob(ctx, job, files, kind, backend, &cfg)

	respond(w, 202, map[string]string{"job_id": jobID})
}

func (s *Server) runAnalyzeJob(ctx context.Context, job *Job, files []string, kind, backend string, cfg *config.Config) {
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

		var (
			result audio.Analysis
			err    error
		)
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
