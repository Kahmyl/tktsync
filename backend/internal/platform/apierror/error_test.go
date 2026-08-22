package apierror

import (
	"net/http"
	"testing"
)

func TestHTTPStatusMapping(t *testing.T) {
	if got := DefaultHTTPStatus(CodeAuthorityTemporarilyUnavailable); got != http.StatusServiceUnavailable {
		t.Fatalf("got %d", got)
	}

	if got := DefaultHTTPStatus(CodeIdempotencyConflict); got != http.StatusConflict {
		t.Fatalf("got %d", got)
	}

	if got := DefaultHTTPStatus(CodeNotAuthorized); got != http.StatusForbidden {
		t.Fatalf("got %d", got)
	}
}
