package outbox

import (
	"testing"
	"time"
)

func TestRetryDelayIsBoundedExponentialWithJitter(t *testing.T) {
	if got := retryDelay(1, 0); got != time.Second {
		t.Fatalf("first retry minimum=%s, want 1s", got)
	}
	if low, high := retryDelay(4, 0), retryDelay(4, 1); low != 6400*time.Millisecond || high != 9600*time.Millisecond {
		t.Fatalf("attempt four jitter range=%s..%s", low, high)
	}
	if got := retryDelay(100, 1); got != 5*time.Minute {
		t.Fatalf("maximum retry=%s, want 5m", got)
	}
	if retryDelay(5, 0.5) <= retryDelay(4, 0.5) {
		t.Fatal("retry delay did not grow exponentially")
	}
}
