package instance

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestIsOursCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{name: "release binary", cmd: "/Users/me/dist/beatportdl-ui", want: true},
		{name: "go run dir", cmd: "/var/folders/xx/exe/beatport-download", want: true},
		{name: "windows exe", cmd: `C:\dist\beatportdl-ui-windows-amd64.exe`, want: true},
		{name: "foreign", cmd: "/usr/sbin/nginx", want: false},
		{name: "empty", cmd: "", want: false},
		{name: "test binary", cmd: "/tmp/instance.test", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsOursCommand(tt.cmd); got != tt.want {
				t.Fatalf("IsOursCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestWriteAndRemovePid(t *testing.T) {
	dir := t.TempDir()
	origDir := pidDir
	origPID := osGetpid
	t.Cleanup(func() {
		pidDir = origDir
		osGetpid = origPID
	})
	pidDir = func() string { return dir }
	osGetpid = func() int { return 4242 }

	if err := WritePid(8989); err != nil {
		t.Fatalf("WritePid: %v", err)
	}
	path := filepath.Join(dir, "server-8989.pid")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "4242\n" {
		t.Fatalf("pid file = %q", data)
	}
	if err := RemoveIfOwned(8989); err != nil {
		t.Fatalf("RemoveIfOwned: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("pid file still present: %v", err)
	}
}

func TestRemoveIfOwned_leavesOtherPid(t *testing.T) {
	dir := t.TempDir()
	origDir := pidDir
	origPID := osGetpid
	t.Cleanup(func() {
		pidDir = origDir
		osGetpid = origPID
	})
	pidDir = func() string { return dir }
	osGetpid = func() int { return 1 }
	if err := os.WriteFile(PidPath(77), []byte("99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIfOwned(77); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(PidPath(77)); err != nil {
		t.Fatalf("expected pid file kept: %v", err)
	}
}

func TestTakeover_signalsOurProcess(t *testing.T) {
	restore := stubProcess(t)
	defer restore()

	var signaled int
	alive := true
	processAlive = func(pid int) bool { return pid == 42 && alive }
	processCommand = func(int) string { return "/tmp/beatportdl-ui" }
	signalTerm = func(pid int) error {
		signaled = pid
		alive = false
		return nil
	}
	portPIDs = func(int) []int { return []int{42} }

	if err := Takeover(context.Background(), 8989); err != nil {
		t.Fatalf("Takeover: %v", err)
	}
	if signaled != 42 {
		t.Fatalf("signaled %d, want 42", signaled)
	}
}

func TestTakeover_refusesForeignProcess(t *testing.T) {
	restore := stubProcess(t)
	defer restore()

	var signaled int
	processAlive = func(int) bool { return true }
	processCommand = func(int) string { return "/usr/sbin/nginx" }
	signalTerm = func(pid int) error {
		signaled = pid
		return nil
	}
	portPIDs = func(int) []int { return []int{99} }

	err := Takeover(context.Background(), 8989)
	if err == nil {
		t.Fatal("expected error")
	}
	if signaled != 0 {
		t.Fatalf("signaled foreign pid %d", signaled)
	}
}

func TestTakeover_trustedPidFileWithoutCommand(t *testing.T) {
	restore := stubProcess(t)
	defer restore()

	if err := os.WriteFile(PidPath(8989), []byte("7\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	alive := true
	processAlive = func(pid int) bool { return pid == 7 && alive }
	processCommand = func(int) string { return "" }
	signalTerm = func(pid int) error {
		if pid != 7 {
			t.Fatalf("pid %d", pid)
		}
		alive = false
		return nil
	}
	portPIDs = func(int) []int { return nil }

	if err := Takeover(context.Background(), 8989); err != nil {
		t.Fatalf("Takeover: %v", err)
	}
}

func TestTakeover_emptyCommandFromLsofRefused(t *testing.T) {
	restore := stubProcess(t)
	defer restore()

	processAlive = func(int) bool { return true }
	processCommand = func(int) string { return "" }
	signalTerm = func(int) error {
		t.Fatal("should not signal")
		return nil
	}
	portPIDs = func(int) []int { return []int{3} }

	if err := Takeover(context.Background(), 8989); err == nil {
		t.Fatal("expected error")
	}
}

func TestIsAddrInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	_, err = net.Listen("tcp", ln.Addr().String())
	if err == nil {
		t.Fatal("expected bind error")
	}
	if !IsAddrInUse(err) {
		t.Fatalf("IsAddrInUse(%v) = false", err)
	}
	if IsAddrInUse(nil) {
		t.Fatal("nil should be false")
	}
	if IsAddrInUse(errors.New("nope")) {
		t.Fatal("unrelated error")
	}
}

func TestServe_GracefulShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, ln) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, getErr := http.Get("http://" + ln.Addr().String() + "/")
		if getErr == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became ready: %v", getErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
}

func TestListen_writesPid(t *testing.T) {
	dir := t.TempDir()
	origDir := pidDir
	t.Cleanup(func() { pidDir = origDir })
	pidDir = func() string { return dir }

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	ln, err := Listen(context.Background(), port)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	defer RemoveIfOwned(port)

	data, err := os.ReadFile(PidPath(port))
	if err != nil {
		t.Fatal(err)
	}
	want := strconv.Itoa(os.Getpid()) + "\n"
	if string(data) != want {
		t.Fatalf("pid file = %q, want %q", data, want)
	}
}

func TestListen_takeoverThenBinds(t *testing.T) {
	restore := stubProcess(t)
	defer restore()

	origListen := listenTCP
	origBind := bindWait
	t.Cleanup(func() {
		listenTCP = origListen
		bindWait = origBind
	})
	bindWait = 50 * time.Millisecond

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	calls := 0
	listenTCP = func(string) (net.Listener, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("listen tcp :8989: bind: address already in use")
		}
		return ln, nil
	}

	alive := true
	processAlive = func(pid int) bool { return pid == 42 && alive }
	processCommand = func(int) string { return "/tmp/beatportdl-ui" }
	signalTerm = func(int) error {
		alive = false
		return nil
	}
	portPIDs = func(int) []int { return []int{42} }

	got, err := Listen(context.Background(), 8989)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if got != ln {
		t.Fatal("expected reused listener")
	}
	if calls != 2 {
		t.Fatalf("listen calls = %d, want 2", calls)
	}
}

func stubProcess(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	origDir := pidDir
	origAlive := processAlive
	origCommand := processCommand
	origTerm := signalTerm
	origPIDs := portPIDs
	origErr := stderr
	origWait := takeoverWait
	origPoll := waitPoll

	pidDir = func() string { return dir }
	stderr = io.Discard
	takeoverWait = time.Second
	waitPoll = time.Millisecond

	return func() {
		pidDir = origDir
		processAlive = origAlive
		processCommand = origCommand
		signalTerm = origTerm
		portPIDs = origPIDs
		stderr = origErr
		takeoverWait = origWait
		waitPoll = origPoll
	}
}
