package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KirillKrainov22/tripgo/internal/config"
	"github.com/KirillKrainov22/tripgo/internal/httpapi"
	"github.com/KirillKrainov22/tripgo/internal/postgres"
	"github.com/KirillKrainov22/tripgo/internal/trip"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration:", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).With("service", "trip-service")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("service stopped with an error", "error", err)
		os.Exit(1)
	}
}

type databaseConfigError struct {
	cause error
}

func (e *databaseConfigError) Error() string {
	return "parse DATABASE_URL: invalid PostgreSQL connection configuration; check URL/DSN syntax and connection options"
}

func (e *databaseConfigError) Unwrap() error {
	return e.cause
}

func openDatabase(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, &databaseConfigError{cause: err}
	}
	poolConfig.MaxConns = cfg.DatabaseMaxConns
	poolConfig.MinConns = cfg.DatabaseMinConns
	poolConfig.MaxConnLifetime = cfg.DatabaseMaxConnLifetime
	poolConfig.ConnConfig.ConnectTimeout = cfg.DatabaseConnectTimeout

	startupCtx, cancelStartup := context.WithTimeout(ctx, cfg.DatabaseConnectTimeout)
	defer cancelStartup()
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

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := openDatabase(ctx, cfg)
	if err != nil {
		return err
	}

	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		pool.Close()
		return fmt.Errorf("listen on HTTP_ADDR: %w", err)
	}

	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	repository := postgres.NewRepository(pool, cfg.DatabaseQueryTimeout)
	txManager := postgres.NewTxManager(pool, cfg.DatabaseQueryTimeout)
	trips := trip.NewService(repository, txManager, cfg.IdempotencyTTL)
	server := &http.Server{
		Handler:           httpapi.NewHandler(trips, pool.Ping, cfg.DatabaseQueryTimeout, logger),
		ReadTimeout:       cfg.HTTPReadTimeout,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
		BaseContext:       func(net.Listener) context.Context { return requestCtx },
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	serveErrors := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErrors <- err
	}()
	logger.Info("HTTP server started", "address", listener.Addr().String())

	var serveErr error
	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case serveErr = <-serveErrors:
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err := shutdown(shutdownCtx, server, pool, cancelRequests); err != nil {
		return errors.Join(serveErr, err)
	}
	if serveErr != nil {
		return fmt.Errorf("serve HTTP: %w", serveErr)
	}
	logger.Info("service stopped")
	return nil
}

func shutdown(ctx context.Context, server *http.Server, pool *pgxpool.Pool, cancelRequests context.CancelFunc) error {
	serverErr := server.Shutdown(ctx)
	if serverErr != nil {
		cancelRequests()
		_ = server.Close()
	}

	closed := make(chan struct{})
	go func() {
		pool.Close()
		close(closed)
	}()
	select {
	case <-closed:
		if serverErr != nil {
			return fmt.Errorf("HTTP shutdown: %w", serverErr)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutdown exceeded SHUTDOWN_TIMEOUT: %w", ctx.Err())
	}
}
