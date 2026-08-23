package telemetry

import "testing"

func TestRouteClassBoundsIdentifierCardinality(t *testing.T) {
	got := routeClass("/api/v1/admin/events/evt_01J8V3TQHZXCN3D06ZJ5K8P9WB/report")
	if got != "/api/v1/admin/events/{id}/report" {
		t.Fatalf("route class=%q", got)
	}
}
