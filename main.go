package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"beatportdl-ui/internal/config"
	"beatportdl-ui/internal/instance"
	"beatportdl-ui/internal/logging"
	"beatportdl-ui/internal/server"
)

//go:embed web
var webFS embed.FS

func main() {
	portFlag := flag.Int("port", 0, "Port to listen on (overrides config)")
	noOpen := flag.Bool("no-open", false, "Don't open browser on start")
	flag.Parse()

	logging.Init()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
		cfg = config.DefaultConfig()
	}

	if *portFlag > 0 {
		cfg.Port = *portFlag
	}

	ctx, stop := instance.NotifyShutdown(context.Background())
	defer stop()

	srv := server.NewServer(cfg)
	mux := http.NewServeMux()
	srv.Mount(mux, webFS)

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           logging.HTTPMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := instance.Listen(ctx, cfg.Port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
	defer instance.RemoveIfOwned(cfg.Port)

	url := fmt.Sprintf("http://localhost:%d", cfg.Port)
	fmt.Printf("BeatportDL UI  →  %s\n", url)

	if !*noOpen {
		go openBrowser(url)
	}

	if err := instance.Serve(ctx, httpSrv, ln); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
	if err := srv.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: analysis store close: %v\n", err)
	}
}

func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	exec.Command(cmd, args...).Start()
}
