package logging

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

func Init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// BeatportAPI logs an outbound HTTP call to the Beatport API (api.beatport.com).
func BeatportAPI(method, reqURL string, status int, ms int64, err error) {
	if !strings.Contains(reqURL, "api.beatport.com") && !strings.Contains(reqURL, "api.beatsource.com") {
		return
	}

	attrs := []any{
		"method", method,
		"url", reqURL,
		"ms", ms,
	}
	if status > 0 {
		attrs = append(attrs, "status", status)
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
		slog.Warn("beatport api", attrs...)
		return
	}
	if status >= 400 {
		slog.Warn("beatport api", attrs...)
		return
	}
	slog.Info("beatport api", attrs...)
}

func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if r.URL.Path == "/api/ws" {
			return
		}
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", rw.status,
			"ms", time.Since(start).Milliseconds(),
		)
	})
}
