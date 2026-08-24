package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type Pinger interface{ Ping(context.Context) error }
type PoolSnapshot struct {
	Acquired, Idle, Total, Max      int32
	AcquireCount, EmptyAcquireCount uint64
	AcquireDuration                 time.Duration
}
type Options struct {
	Readiness              *Readiness
	RequestTimeout         time.Duration
	LongRequestTimeout     time.Duration
	MaxBodyBytes           int64
	MaxInFlight            int
	RealtimeMaxConnections int
	MetricsEnabled         bool
	MetricsToken           string
	PoolStats              func() PoolSnapshot
}
type Readiness struct{ draining atomic.Bool }

func (r *Readiness) BeginDrain() {
	if r != nil {
		r.draining.Store(true)
	}
}
func (r *Readiness) IsDraining() bool { return r != nil && r.draining.Load() }

type RuntimeMetrics struct {
	inFlight                                          atomic.Int64
	requests, errors, panics, rejected, durationNanos atomic.Uint64
	realtimeConnections                               atomic.Int64
	realtimeConnects, realtimeRejected                atomic.Uint64
	latency                                           latencyHistogram
}

type contextKey string

const requestIDKey contextKey = "request_id"

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}
func CorrelationID(ctx context.Context) string { return RequestID(ctx) }
func CorrelationUUID(ctx context.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(RequestID(ctx))
	return id, err == nil
}

func Handler(logger *slog.Logger, database Pinger, apiHandlers ...http.Handler) http.Handler {
	return HandlerWithOptions(logger, database, Options{}, apiHandlers...)
}

func HandlerWithOptions(logger *slog.Logger, database Pinger, options Options, apiHandlers ...http.Handler) http.Handler {
	if options.Readiness == nil {
		options.Readiness = &Readiness{}
	}
	metrics := &RuntimeMetrics{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if options.Readiness.IsDraining() {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "reason": "draining"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if database == nil || database.Ping(ctx) != nil {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "database": "unavailable"})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ready", "database": "available"})
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		if !options.MetricsEnabled {
			http.NotFound(w, r)
			return
		}
		if !constantBearer(r.Header.Get("Authorization"), options.MetricsToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeMetrics(w, metrics, options)
	})
	for _, handler := range apiHandlers {
		if handler != nil {
			mux.Handle("/api/v1/", handler)
			break
		}
	}
	return requestLogging(
		logger,
		metrics,
		recoverPanics(
			logger,
			metrics,
			limitRealtimeConcurrency(
				metrics,
				options.RealtimeMaxConnections,
				limitConcurrency(
					metrics,
					options.MaxInFlight,
					limitBody(
						options.MaxBodyBytes,
						requestDeadline(
							options.RequestTimeout,
							options.LongRequestTimeout,
							mux,
						),
					),
				),
			),
		),
	)
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr, ok := apierror.As(err)
	if !ok {
		apiErr = apierror.New(apierror.CodeInternal, "an internal error occurred")
	}
	WriteJSON(w, apiErr.HTTPStatus, map[string]any{"error": map[string]any{"code": apiErr.Code, "message": apiErr.Message, "request_id": RequestID(r.Context()), "details": apiErr.Details}})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func requestLogging(logger *slog.Logger, metrics *RuntimeMetrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := r.Header.Get("X-Request-ID")
		if parsed, err := uuid.Parse(requestID); err == nil {
			requestID = parsed.String()
		} else {
			requestID = uuid.NewString()
		}
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID))
		w.Header().Set("X-Request-ID", requestID)
		wrapped := &statusWriter{ResponseWriter: w}
		metrics.requests.Add(1)
		realtime := r.URL.Path == "/api/v1/realtime/stream"
		if !realtime {
			metrics.inFlight.Add(1)
		}
		defer func() {
			if !realtime {
				metrics.inFlight.Add(-1)
			}
			elapsed := time.Since(started)
			metrics.durationNanos.Add(uint64(elapsed))
			status := wrapped.status
			if status == 0 {
				status = 200
			}
			if status >= 500 {
				metrics.errors.Add(1)
			}
			metrics.observeLatency(
				r.Method,
				routeClass(r.URL.Path),
				status,
				elapsed,
			)
			logger.InfoContext(r.Context(), "request completed", "request_id", requestID, "correlation_id", requestID, "operation", r.Method+" "+routeClass(r.URL.Path), "status", status, "duration", elapsed)
		}()
		next.ServeHTTP(wrapped, r)
	})
}

func recoverPanics(logger *slog.Logger, metrics *RuntimeMetrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				metrics.panics.Add(1)
				logger.ErrorContext(r.Context(), "request panic recovered", "request_id", RequestID(r.Context()), "operation", r.Method+" "+routeClass(r.URL.Path), "panic_type", fmt.Sprintf("%T", recovered), "stack", string(debug.Stack()))
				WriteError(w, r, apierror.New(apierror.CodeInternal, "an internal error occurred"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func limitConcurrency(metrics *RuntimeMetrics, maximum int, next http.Handler) http.Handler {
	if maximum <= 0 {
		return next
	}
	slots := make(chan struct{}, maximum)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isOperational(r.URL.Path) ||
			r.URL.Path == "/api/v1/realtime/stream" {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			next.ServeHTTP(w, r)
		default:
			metrics.rejected.Add(1)
			w.Header().Set("Retry-After", "1")
			WriteError(w, r, apierror.WithStatus(apierror.CodeAuthorityTemporarilyUnavailable, "service is at request capacity", http.StatusServiceUnavailable))
		}
	})
}
func limitRealtimeConcurrency(
	metrics *RuntimeMetrics,
	maximum int,
	next http.Handler,
) http.Handler {
	if maximum <= 0 {
		return next
	}

	slots := make(chan struct{}, maximum)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/realtime/stream" {
			next.ServeHTTP(w, r)
			return
		}

		select {
		case slots <- struct{}{}:
			metrics.realtimeConnections.Add(1)
			metrics.realtimeConnects.Add(1)
			defer func() {
				metrics.realtimeConnections.Add(-1)
				<-slots
			}()
			next.ServeHTTP(w, r)
		default:
			metrics.realtimeRejected.Add(1)
			w.Header().Set("Retry-After", "1")
			WriteError(
				w,
				r,
				apierror.WithStatus(
					apierror.CodeAuthorityTemporarilyUnavailable,
					"realtime connection capacity reached",
					http.StatusServiceUnavailable,
				),
			)
		}
	})
}

func limitBody(maximum int64, next http.Handler) http.Handler {
	if maximum <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maximum)
		}
		next.ServeHTTP(w, r)
	})
}
func requestDeadline(timeout, longTimeout time.Duration, next http.Handler) http.Handler {
	if timeout <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/realtime/stream" || isOperational(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		selected := timeout
		if longTimeout > 0 && (strings.Contains(r.URL.Path, "/reports/") || strings.HasSuffix(r.URL.Path, "/audit") || strings.HasSuffix(r.URL.Path, "/accreditation-export")) {
			selected = longTimeout
		}
		ctx, cancel := context.WithTimeout(r.Context(), selected)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func isOperational(path string) bool {
	return path == "/health" || path == "/ready" || path == "/metrics"
}
func routeClass(path string) string {
	if !strings.HasPrefix(path, "/api/v1/") {
		return path
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range parts {
		if (strings.Contains(p, "_") || strings.Contains(p, "-")) && len(p) > 20 {
			parts[i] = "{id}"
		}
	}
	return "/" + strings.Join(parts, "/")
}
func constantBearer(header, token string) bool {
	header = strings.TrimSpace(header)
	return token != "" && subtleEqual(header, "Bearer "+token)
}
func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

func writeMetrics(w http.ResponseWriter, m *RuntimeMetrics, o Options) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	requests := m.requests.Load()
	avg := float64(0)
	if requests > 0 {
		avg = float64(m.durationNanos.Load()) / float64(requests) / float64(time.Second)
	}
	_, _ = fmt.Fprintf(w, "# TYPE tktsync_http_requests_total counter\ntktsync_http_requests_total %d\n# TYPE tktsync_http_errors_total counter\ntktsync_http_errors_total %d\n# TYPE tktsync_http_in_flight gauge\ntktsync_http_in_flight %d\n# TYPE tktsync_http_rejected_total counter\ntktsync_http_rejected_total %d\n# TYPE tktsync_http_panics_total counter\ntktsync_http_panics_total %d\n# TYPE tktsync_http_duration_average_seconds gauge\ntktsync_http_duration_average_seconds %.9f\n# TYPE tktsync_realtime_connections gauge\ntktsync_realtime_connections %d\n# TYPE tktsync_realtime_connects_total counter\ntktsync_realtime_connects_total %d\n# TYPE tktsync_realtime_rejected_total counter\ntktsync_realtime_rejected_total %d\n# TYPE go_goroutines gauge\ngo_goroutines %d\n# TYPE go_heap_alloc_bytes gauge\ngo_heap_alloc_bytes %d\n# TYPE go_gc_cycles_total counter\ngo_gc_cycles_total %d\n", requests, m.errors.Load(), m.inFlight.Load(), m.rejected.Load(), m.panics.Load(), avg, m.realtimeConnections.Load(), m.realtimeConnects.Load(), m.realtimeRejected.Load(), runtime.NumGoroutine(), memory.HeapAlloc, memory.NumGC)
	m.writeLatency(w)
	if o.PoolStats != nil {
		s := o.PoolStats()
		_, _ = fmt.Fprintf(w, "# TYPE tktsync_db_pool_connections gauge\ntktsync_db_pool_connections{state=\"acquired\"} %d\ntktsync_db_pool_connections{state=\"idle\"} %d\ntktsync_db_pool_connections{state=\"total\"} %d\ntktsync_db_pool_connections{state=\"max\"} %d\n# TYPE tktsync_db_pool_acquires_total counter\ntktsync_db_pool_acquires_total %d\n# TYPE tktsync_db_pool_empty_acquires_total counter\ntktsync_db_pool_empty_acquires_total %d\n# TYPE tktsync_db_pool_acquire_seconds_total counter\ntktsync_db_pool_acquire_seconds_total %.9f\n", s.Acquired, s.Idle, s.Total, s.Max, s.AcquireCount, s.EmptyAcquireCount, s.AcquireDuration.Seconds())
	}
}
