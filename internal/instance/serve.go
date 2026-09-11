package instance

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"time"
)

const shutdownTimeout = 10 * time.Second

// NotifyShutdown is cancelled on SIGINT/SIGTERM (Windows: Ctrl+C).
func NotifyShutdown(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, shutdownSignals...)
}

// Serve runs srv on ln until ctx is cancelled, then shuts down gracefully.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		err := srv.Shutdown(shutdownCtx)
		serveErr := <-errCh
		if err != nil {
			_ = srv.Close()
			return fmt.Errorf("shutdown: %w", err)
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
