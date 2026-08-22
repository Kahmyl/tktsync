package webhook

import "testing"

func TestProductionDestinationValidationRejectsSSRFAddresses(t *testing.T) {
	for _, raw := range []string{"http://example.com/hook", "https://127.0.0.1/hook", "https://169.254.169.254/latest", "https://user:pass@example.com/hook"} {
		if _, err := validateDestination(raw, false); err == nil {
			t.Fatalf("destination %q unexpectedly accepted", raw)
		}
	}
	if got, err := validateDestination("https://hooks.example.com/tktsync", false); err != nil || got == "" {
		t.Fatalf("public HTTPS rejected: %v", err)
	}
}
