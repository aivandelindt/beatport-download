package server

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
)

func (s *Server) Mount(mux *http.ServeMux, webFS embed.FS) {
	// API routes
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/settings", s.handleSaveSettings)
	mux.HandleFunc("POST /api/auth/test", s.handleTestAuth)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/genres", s.handleGenres)
	mux.HandleFunc("POST /api/download", s.handleDownload)
	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleDeleteJob)
	mux.HandleFunc("GET /api/jobs/{id}/zip", s.handleJobZip)
	mux.HandleFunc("POST /api/fix", s.handleFix)
	mux.HandleFunc("GET /api/ws", s.handleWS)

	// Static web UI
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(fmt.Sprintf("failed to create web sub-fs: %v", err))
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("/", fileServer)
}
