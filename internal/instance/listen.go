package instance

import (
	"context"
	"fmt"
	"net"
	"time"
)

var listenTCP = func(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

var bindWait = 2 * time.Second

func finishListen(port int, ln net.Listener) (net.Listener, error) {
	if err := WritePid(port); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// Listen binds TCP :port, taking over a previous BeatportDL-UI instance if needed.
func Listen(ctx context.Context, port int) (net.Listener, error) {
	addr := fmt.Sprintf(":%d", port)
	ln, err := listenTCP(addr)
	if err == nil {
		return finishListen(port, ln)
	}
	if !IsAddrInUse(err) {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	if err := Takeover(ctx, port); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(bindWait)
	for {
		ln, err := listenTCP(addr)
		if err == nil {
			return finishListen(port, ln)
		}
		if !IsAddrInUse(err) {
			return nil, fmt.Errorf("listen %s: %w", addr, err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("listen %s: %w", addr, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waitPoll):
		}
	}
}
