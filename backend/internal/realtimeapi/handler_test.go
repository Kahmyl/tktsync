package realtimeapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabledRealtimeFailsClosed(
	t *testing.T,
) {
	handler :=
		New(nil, nil, nil, nil, false)

	request :=
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/realtime/stream?audience=admin&event_id=evt_test",
			nil,
		)

	response :=
		httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code !=
		http.StatusServiceUnavailable {
		t.Fatalf(
			"status=%d want=%d body=%s",
			response.Code,
			http.StatusServiceUnavailable,
			response.Body.String(),
		)
	}

	if !strings.Contains(
		response.Body.String(),
		"AUTHORITY_TEMPORARILY_UNAVAILABLE",
	) {
		t.Fatalf(
			"unexpected disabled realtime response: %s",
			response.Body.String(),
		)
	}
}

func TestRealtimeDefaultsEnabledForExplicitHandlerUse(
	t *testing.T,
) {
	handler :=
		New(nil, nil, nil, nil)

	request :=
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/realtime/stream",
			nil,
		)

	response :=
		httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		request,
	)

	if response.Code ==
		http.StatusServiceUnavailable &&
		strings.Contains(
			response.Body.String(),
			"realtime streaming is disabled",
		) {
		t.Fatal(
			"default realtime handler unexpectedly disabled",
		)
	}
}
