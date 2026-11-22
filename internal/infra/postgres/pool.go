package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	ConnectTimeout  time.Duration
}

type configError struct {
	cause error
}

func (e *configError) Error() string {
	return "parse DATABASE_URL: invalid PostgreSQL connection configuration; check URL/DSN syntax and connection options"
}

func (e *configError) Unwrap() error {
	return e.cause
}

func NewPool(ctx context.Context, cfg Config, tracerProvider trace.TracerProvider, meter metric.Meter) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, &configError{cause: err}
	}
	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	poolConfig.MaxConnLifetime = cfg.MaxConnLifetime
	poolConfig.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	tracer, err := newQueryTracer(tracerProvider, meter)
	if err != nil {
		return nil, err
	}
	poolConfig.ConnConfig.Tracer = tracer

	startupCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(startupCtx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(startupCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("check database on startup: %w", err)
	}
	return pool, nil
}
