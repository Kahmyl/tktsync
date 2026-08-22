package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSUsesExactAllowlist(t *testing.T) {
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }), []string{"https://select.example"})
	for _, test := range []struct {
		origin string
		want   int
		allow  bool
	}{{"https://select.example", 204, true}, {"https://evil.example", 403, false}} {
		request := httptest.NewRequest(http.MethodOptions, "/api/v1/selection/session", nil)
		request.Header.Set("Origin", test.origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("origin %s status=%d", test.origin, response.Code)
		}
		if (response.Header().Get("Access-Control-Allow-Origin") != "") != test.allow {
			t.Fatalf("origin %s allow header mismatch", test.origin)
		}
	}
}
