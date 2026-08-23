package worker

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type testPinger struct{}

func (testPinger) Ping(context.Context) error { return nil }

type testJob struct {
	active, maximum, calls atomic.Int32
	release                <-chan struct{}
}

func (j *testJob) RunOnce(ctx context.Context) error {
	j.calls.Add(1)
	current := j.active.Add(1)
	defer j.active.Add(-1)
	for {
		previous := j.maximum.Load()
		if current <= previous || j.maximum.CompareAndSwap(previous, current) {
			break
		}
	}
	select {
	case <-j.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRunnerBoundsConcurrencyAndDrainsInflight(t *testing.T) {
	release := make(chan struct{})
	job := &testJob{release: release}
	runner := New(slog.New(slog.NewTextHandler(io.Discard, nil)), testPinger{}, time.Millisecond, 100*time.Millisecond, 2, job)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	deadline := time.After(time.Second)
	for job.calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("jobs did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if got := job.maximum.Load(); got != 2 {
		t.Fatalf("maximum concurrency=%d want=2", got)
	}
	cancel()
	time.Sleep(5 * time.Millisecond)
	if job.calls.Load() != 2 {
		t.Fatal("worker claimed new work after cancellation")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not drain")
	}
}

func TestRunnerCancelsInflightAtDrainDeadline(t *testing.T) {
	release := make(chan struct{})
	job := &testJob{release: release}
	runner := New(slog.New(slog.NewTextHandler(io.Discard, nil)), testPinger{}, time.Hour, 5*time.Millisecond, 1, job)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	for job.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected drain deadline error")
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not enforce drain deadline")
	}
}
