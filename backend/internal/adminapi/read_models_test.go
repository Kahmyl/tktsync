package adminapi

import (
	"net/http/httptest"
	"testing"
)

func TestAdminPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
		wantError  bool
	}{
		{name: "defaults", wantLimit: defaultAdminPageSize},
		{name: "bounded values", query: "?limit=100&offset=25", wantLimit: 100, wantOffset: 25},
		{name: "limit too large", query: "?limit=101", wantError: true},
		{name: "zero limit", query: "?limit=0", wantError: true},
		{name: "negative offset", query: "?offset=-1", wantError: true},
		{name: "invalid number", query: "?limit=many", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest("GET", "/api/v1/admin/events"+test.query, nil)
			limit, offset, err := adminPage(request)
			if test.wantError {
				if err == nil {
					t.Fatal("adminPage() error = nil, want validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("adminPage() error = %v", err)
			}
			if limit != test.wantLimit || offset != test.wantOffset {
				t.Fatalf("adminPage() = (%d, %d), want (%d, %d)", limit, offset, test.wantLimit, test.wantOffset)
			}
		})
	}
}
