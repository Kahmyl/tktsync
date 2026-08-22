package database

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsRetryableTransactionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"deadlock", &pgconn.PgError{Code: "40P01"}, true},
		{"serialization", &pgconn.PgError{Code: "40001"}, true},
		{"unique violation", &pgconn.PgError{Code: "23505"}, false},
		{"ordinary error", errors.New("boom"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryableTransactionError(tc.err); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSortUUIDsIsDeterministic(t *testing.T) {
	a := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	c := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	got := SortUUIDs([]uuid.UUID{a, b, c})

	if got[0] != b || got[1] != c || got[2] != a {
		t.Fatalf("unexpected order: %#v", got)
	}
}
