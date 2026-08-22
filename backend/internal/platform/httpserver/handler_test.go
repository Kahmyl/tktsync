package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

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

	Handler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		fakePinger{err: errors.New("down")},
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

			Handler(
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				fakePinger{err: tc.err},
			).ServeHTTP(response, request)

			if response.Code != tc.want {
				t.Fatalf("got status %d, want %d", response.Code, tc.want)
			}
		})
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
		next,
	).ServeHTTP(response, request)

	if _, err := uuid.Parse(requestID); err != nil {
		t.Fatalf("generated request ID is not UUID: %q", requestID)
	}

	if response.Header().Get("X-Request-ID") != requestID {
		t.Fatal("response request ID does not match request context")
	}
}
