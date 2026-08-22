package database

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type transactionContextKey struct{}

func WithTransaction(
	ctx context.Context,
	tx pgx.Tx,
) context.Context {
	return context.WithValue(
		ctx,
		transactionContextKey{},
		tx,
	)
}

func TransactionFromContext(
	ctx context.Context,
) (pgx.Tx, bool) {
	tx, ok := ctx.Value(
		transactionContextKey{},
	).(pgx.Tx)

	return tx, ok
}
