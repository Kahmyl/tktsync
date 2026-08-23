package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PoolOptions struct {
	URL, ApplicationName                         string
	MaxConnections, MinConnections               int32
	MaxLifetime, MaxIdleLifetime, ConnectTimeout time.Duration
	StatementTimeout, LockTimeout                time.Duration
}

func Open(ctx context.Context, options PoolOptions) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(options.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database configuration: %w", err)
	}
	poolConfig.MaxConns = options.MaxConnections
	poolConfig.MinConns = options.MinConnections
	poolConfig.MaxConnLifetime = options.MaxLifetime
	poolConfig.MaxConnIdleTime = options.MaxIdleLifetime
	poolConfig.ConnConfig.ConnectTimeout = options.ConnectTimeout
	poolConfig.ConnConfig.RuntimeParams["application_name"] = options.ApplicationName
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = durationMilliseconds(options.StatementTimeout)
	poolConfig.ConnConfig.RuntimeParams["lock_timeout"] = durationMilliseconds(options.LockTimeout)
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	startup, cancel := context.WithTimeout(ctx, options.ConnectTimeout)
	defer cancel()
	if err = pool.Ping(startup); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	return pool, nil
}

func durationMilliseconds(value time.Duration) string {
	return fmt.Sprintf("%d", value.Milliseconds())
}
