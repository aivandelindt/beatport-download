package main

import (
	"embed"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"beatportdl-ui/internal/config"
	"beatportdl-ui/internal/server"
)

//go:embed web
var webFS embed.FS

func main() {
	portFlag := flag.Int("port", 0, "Port to listen on (overrides config)")
	noOpen := flag.Bool("no-open", false, "Don't open browser on start")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
		cfg = config.DefaultConfig()
	}

	if *portFlag > 0 {
		cfg.Port = *portFlag
	}

	srv := server.NewServer(cfg)
	mux := http.NewServeMux()
	srv.Mount(mux, webFS)

	addr := fmt.Sprintf(":%d", cfg.Port)
	url := fmt.Sprintf("http://localhost:%d", cfg.Port)

	fmt.Printf("BeatportDL UI  →  %s\n", url)

	if !*noOpen {
		go openBrowser(url)
	}

	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
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
