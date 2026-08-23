package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestServerCancellationBeginsDrainAndStops(t *testing.T) {
	readiness := &Readiness{}
	server := New("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), slog.New(slog.NewTextHandler(io.Discard, nil)), ServerOptions{ShutdownTimeout: time.Second, ReadHeaderTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1 << 20, Readiness: readiness})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop")
	}
	if !readiness.IsDraining() {
		t.Fatal("readiness did not transition to draining")
	}
}
