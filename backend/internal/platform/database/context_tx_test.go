package database

import (
	"context"
	"testing"
)

func TestTransactionContextAbsentByDefault(
	t *testing.T,
) {
	if _, ok := TransactionFromContext(
		context.Background(),
	); ok {
		t.Fatal("unexpected transaction in empty context")
	}
}
