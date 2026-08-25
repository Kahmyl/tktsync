package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakePinger struct {
	err error
}

func (p fakePinger) Ping(context.Context) error {
	return p.err
}

func TestHealthDoesNotDependOnDatabase(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	HandlerWithOptions(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		fakePinger{err: errors.New("down")},
		Options{},
	).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("got status %d", response.Code)
	}
}

func TestReadinessReflectsDatabase(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"ready", nil, http.StatusOK},
		{"unavailable", errors.New("down"), http.StatusServiceUnavailable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/ready", nil)
			response := httptest.NewRecorder()

			HandlerWithOptions(
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				fakePinger{err: tc.err},
				Options{},
			).ServeHTTP(response, request)

			if response.Code != tc.want {
				t.Fatalf("got status %d, want %d", response.Code, tc.want)
			}
		})
	}
}

func TestReadinessTurnsOffWhileDraining(t *testing.T) {
	state := &Readiness{}
	handler := HandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), fakePinger{}, Options{Readiness: state})
	state.BeginDrain()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("got status %d", response.Code)
	}
}

func TestRequestBodyLimit(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value any
		err := json.NewDecoder(r.Body).Decode(&value)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := HandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), fakePinger{}, Options{MaxBodyBytes: 8}, next)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/example", strings.NewReader(`{"value":"too large"}`)))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got status %d", response.Code)
	}
}

func TestRequestPanicIsContained(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })
	handler := HandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), fakePinger{}, Options{}, next)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/example", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "boom") {
		t.Fatal("panic detail leaked")
	}
}

func TestRouteClassRedactsTicketQRCapability(t *testing.T) {
	capability := "tqp1_AAAAAf3Qj2uRSOaP-yjViKRBdABm3pkQHVGTQ5ayTwGuQRnYfjJk-LJQ_vdpM4Cmu9kOaA"
	classified := routeClass("/api/v1/ticket-qr/" + capability)
	if classified != "/api/v1/ticket-qr/{capability}" {
		t.Fatalf("route class=%q", classified)
	}
	if strings.Contains(classified, capability) {
		t.Fatal("ticket QR capability leaked into route class")
	}
}

func TestRequestDeadlinePropagatesToHandler(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusGatewayTimeout)
	})
	handler := HandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), fakePinger{}, Options{RequestTimeout: time.Millisecond}, next)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/example", nil))
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("got status %d", response.Code)
	}
}

func TestReportRoutesUseLongerDeadlineClass(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		if r.Context().Err() != nil {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	handler := HandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), fakePinger{}, Options{RequestTimeout: time.Millisecond, LongRequestTimeout: 50 * time.Millisecond}, next)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/events/evt_01J8V3TQHZXCN3D06ZJ5K8P9WB/reports/inventory", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("got status %d", response.Code)
	}
}

func TestMetricsRequireBearer(t *testing.T) {
	handler := HandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), fakePinger{}, Options{MetricsEnabled: true, MetricsToken: "secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d", response.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "tktsync_http_requests_total") {
		t.Fatalf("metrics response %d %q", response.Code, response.Body.String())
	}
}

func TestValidRequestIdentifierIsPreserved(t *testing.T) {
	expected := uuid.NewString()

	request := httptest.NewRequest(http.MethodGet, "/example", nil)
	request.Header.Set("X-Request-ID", expected)

	response := httptest.NewRecorder()

	var requestID string

	next := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		requestID = RequestID(request.Context())
	})

	requestLogging(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		&RuntimeMetrics{},
		next,
	).ServeHTTP(response, request)

	if requestID != expected {
		t.Fatalf("got %q, want %q", requestID, expected)
	}

	if CorrelationID(request.Context()) != "" {
		t.Fatal("original request context must remain untouched")
	}
}

func TestInvalidRequestIdentifierIsReplaced(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/example", nil)
	request.Header.Set("X-Request-ID", "not-a-valid-correlation-id")

	response := httptest.NewRecorder()

	var requestID string

	next := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		requestID = RequestID(request.Context())
	})

	requestLogging(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		&RuntimeMetrics{},
		next,
	).ServeHTTP(response, request)

	if _, err := uuid.Parse(requestID); err != nil {
		t.Fatalf("generated request ID is not UUID: %q", requestID)
	}

	if response.Header().Get("X-Request-ID") != requestID {
		t.Fatal("response request ID does not match request context")
	}
}
