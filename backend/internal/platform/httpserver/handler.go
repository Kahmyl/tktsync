package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type Pinger interface {
	Ping(context.Context) error
}

type contextKey string

const requestIDKey contextKey = "request_id"

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

func CorrelationID(ctx context.Context) string {
	return RequestID(ctx)
}

func CorrelationUUID(
	ctx context.Context,
) (uuid.UUID, bool) {
	value := RequestID(ctx)
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, false
	}

	return id, true
}

func Handler(
	logger *slog.Logger,
	database Pinger,
	apiHandlers ...http.Handler,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(
		"GET /health",
		func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			WriteJSON(
				w,
				http.StatusOK,
				map[string]string{
					"status": "ok",
				},
			)
		},
	)

	mux.HandleFunc(
		"GET /ready",
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			ctx, cancel := context.WithTimeout(
				r.Context(),
				2*time.Second,
			)
			defer cancel()

			if database == nil ||
				database.Ping(ctx) != nil {
				WriteJSON(
					w,
					http.StatusServiceUnavailable,
					map[string]string{
						"status":   "not_ready",
						"database": "unavailable",
					},
				)
				return
			}

			WriteJSON(
				w,
				http.StatusOK,
				map[string]string{
					"status":   "ready",
					"database": "available",
				},
			)
		},
	)

	for _, handler := range apiHandlers {
		if handler == nil {
			continue
		}

		mux.Handle(
			"/api/v1/",
			handler,
		)
		break
	}

	return requestLogging(
		logger,
		mux,
	)
}

func WriteJSON(
	w http.ResponseWriter,
	status int,
	body any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func WriteError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	apiErr, ok := apierror.As(err)
	if !ok {
		apiErr = apierror.New(
			apierror.CodeInternal,
			"an internal error occurred",
		)
	}

	requestID := RequestID(
		r.Context(),
	)

	WriteJSON(
		w,
		apiErr.HTTPStatus,
		map[string]any{
			"error": map[string]any{
				"code":       apiErr.Code,
				"message":    apiErr.Message,
				"request_id": requestID,
				"details":    apiErr.Details,
			},
		},
	)
}

func requestLogging(
	logger *slog.Logger,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			started := time.Now()

			requestID := r.Header.Get(
				"X-Request-ID",
			)

			if parsed, err := uuid.Parse(
				requestID,
			); err == nil {
				requestID = parsed.String()
			} else {
				requestID = uuid.NewString()
			}

			ctx := context.WithValue(
				r.Context(),
				requestIDKey,
				requestID,
			)

			r = r.WithContext(ctx)

			w.Header().Set(
				"X-Request-ID",
				requestID,
			)

			next.ServeHTTP(w, r)

			logger.InfoContext(
				r.Context(),
				"request completed",
				"request_id",
				requestID,
				"correlation_id",
				requestID,
				"operation",
				r.Method+" "+r.URL.Path,
				"duration",
				time.Since(started),
			)
		},
	)
}
