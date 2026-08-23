package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkHTTPRuntimeHealth(b *testing.B) {
	handler := HandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), fakePinger{}, Options{MaxInFlight: 200, MaxBodyBytes: 1 << 20, RequestTimeout: 15 * time.Second})
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
			if response.Code != http.StatusOK {
				b.Fatalf("status %d", response.Code)
			}
		}
	})
}

func TestRuntimeLoadProfile(t *testing.T) {
	if os.Getenv("RUNTIME_BENCHMARK") != "true" {
		t.Skip("set RUNTIME_BENCHMARK=true to collect the local runtime profile")
	}
	handler := HandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), fakePinger{}, Options{MaxInFlight: 200, MaxBodyBytes: 1 << 20, RequestTimeout: 15 * time.Second}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	for _, concurrency := range []int{1, 5, 10, 25, 50, 100, 200} {
		const requests = 1000
		durations := make([]time.Duration, requests)
		var failures atomic.Int64
		started := time.Now()
		jobs := make(chan int)
		var group sync.WaitGroup
		for worker := 0; worker < concurrency; worker++ {
			group.Add(1)
			go func() {
				defer group.Done()
				for index := range jobs {
					requestStarted := time.Now()
					response, err := client.Get(server.URL + "/api/v1/benchmark")
					durations[index] = time.Since(requestStarted)
					if err != nil {
						failures.Add(1)
						continue
					}
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
					if response.StatusCode >= 400 {
						failures.Add(1)
					}
				}
			}()
		}
		for index := 0; index < requests; index++ {
			jobs <- index
		}
		close(jobs)
		group.Wait()
		elapsed := time.Since(started)
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		t.Logf("concurrency=%d throughput=%.1f req/s p50=%s p95=%s p99=%s errors=%d", concurrency, float64(requests)/elapsed.Seconds(), percentile(durations, .50), percentile(durations, .95), percentile(durations, .99), failures.Load())
	}
}

func percentile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * quantile)
	return values[index]
}
