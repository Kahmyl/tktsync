package reporting

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestObserverMetricsAreAdvisoryProcessObservations(t *testing.T) {
	observer := NewObserver()
	handler := observer.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/denied" {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"INVENTORY_UNAVAILABLE"}}`))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/denied", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/conflict", nil))
	metrics := observer.Snapshot()
	if metrics.RequestCount != 2 || metrics.ErrorCount != 2 || metrics.AuthAnomalyCount != 1 || metrics.ConflictResponseCount != 1 || metrics.HoldConflictCount != 1 || metrics.ErrorRate != 1 || metrics.HoldConflictRate != 0.5 {
		t.Fatalf("unexpected metrics: %+v", metrics)
	}
}

func TestObserverPreservesStreamingFlush(t *testing.T) {
	observer := NewObserver()
	handler := observer.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("observer removed http.Flusher")
		}
		flusher.Flush()
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if !recorder.Flushed {
		t.Fatal("observer did not forward stream flush")
	}
}
