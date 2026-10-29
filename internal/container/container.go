package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KirillKrainov22/tripgo/internal/config"
	"github.com/KirillKrainov22/tripgo/internal/http/router"
	tripcreatehttp "github.com/KirillKrainov22/tripgo/internal/http/trip/create"
	tripfinishhttp "github.com/KirillKrainov22/tripgo/internal/http/trip/finish"
	tripgethttp "github.com/KirillKrainov22/tripgo/internal/http/trip/get"
	"github.com/KirillKrainov22/tripgo/internal/infra/postgres"
	"github.com/KirillKrainov22/tripgo/internal/infra/postgres/transaction"
	"github.com/KirillKrainov22/tripgo/internal/infra/telemetry"
	idempotencyrepo "github.com/KirillKrainov22/tripgo/internal/repository/idempotency"
	triprepo "github.com/KirillKrainov22/tripgo/internal/repository/trip"
	"github.com/KirillKrainov22/tripgo/internal/usecase/trip/create"
	"github.com/KirillKrainov22/tripgo/internal/usecase/trip/finish"
	"github.com/KirillKrainov22/tripgo/internal/usecase/trip/get"
	idempotencyworker "github.com/KirillKrainov22/tripgo/internal/worker/idempotency"
)

type App struct {
	logger         *slog.Logger
	telemetry      *telemetry.Telemetry
	pool           *pgxpool.Pool
	listener       net.Listener
	server         *http.Server
	cancelRequests context.CancelFunc
	cleaner        *idempotencyworker.Cleaner
	backgroundCtx  context.Context
	stopBackground context.CancelFunc
	backgroundDone chan struct{}
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	logger := telemetry.NewLogger(os.Stdout, cfg.LogLevel)
	tel, err := telemetry.Setup(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("setup telemetry: %w", err)
	}
	meter := tel.Meter()

	pool, err := postgres.NewPool(ctx, postgres.Config{
		URL:             cfg.DatabaseURL,
		MaxConns:        cfg.DatabaseMaxConns,
		MinConns:        cfg.DatabaseMinConns,
		MaxConnLifetime: cfg.DatabaseMaxConnLifetime,
		ConnectTimeout:  cfg.DatabaseConnectTimeout,
	}, tel.TracerProvider, meter)
	if err != nil {
		return nil, errors.Join(err, tel.Shutdown(ctx))
	}

	app, err := build(ctx, cfg, logger, tel, pool)
	if err != nil {
		pool.Close()
		return nil, errors.Join(err, tel.Shutdown(ctx))
	}
	return app, nil
}

func build(ctx context.Context, cfg config.Config, logger *slog.Logger, tel *telemetry.Telemetry, pool *pgxpool.Pool) (*App, error) {
	meter := tel.Meter()
	trips := triprepo.New(pool, cfg.DatabaseQueryTimeout)
	keys := idempotencyrepo.New(pool, cfg.DatabaseQueryTimeout)
	txManager := transaction.NewManager(pool, cfg.DatabaseTxTimeout)

	tripMetrics, err := telemetry.NewTripMetrics(meter)
	if err != nil {
		return nil, err
	}
	if err := telemetry.RegisterActiveTrips(meter, trips.CountActive); err != nil {
		return nil, err
	}

	handler, err := router.New(router.Config{
		CreateTrip:     tripcreatehttp.New(create.New(trips, keys, txManager, tripMetrics, logger, cfg.IdempotencyTTL), logger),
		GetTrip:        tripgethttp.New(get.New(trips), logger),
		FinishTrip:     tripfinishhttp.New(finish.New(trips, txManager, tripMetrics, logger), logger),
		Ping:           pool.Ping,
		ReadyTimeout:   cfg.DatabaseQueryTimeout,
		Logger:         logger,
		TracerProvider: tel.TracerProvider,
		Meter:          meter,
	})
	if err != nil {
		return nil, err
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on HTTP_ADDR: %w", err)
	}

	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	backgroundCtx, stopBackground := context.WithCancel(context.WithoutCancel(ctx))
	server := &http.Server{
		Handler:           handler,
		ReadTimeout:       cfg.HTTPReadTimeout,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
		BaseContext:       func(net.Listener) context.Context { return requestCtx },
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	return &App{
		logger:         logger,
		telemetry:      tel,
		pool:           pool,
		listener:       listener,
		server:         server,
		cancelRequests: cancelRequests,
		cleaner:        idempotencyworker.NewCleaner(keys, cfg.IdempotencyCleanupInterval, logger),
		backgroundCtx:  backgroundCtx,
		stopBackground: stopBackground,
		backgroundDone: make(chan struct{}),
	}, nil
}

func (a *App) Logger() *slog.Logger {
	return a.logger
}

func (a *App) Start() <-chan error {
	go func() {
		defer close(a.backgroundDone)
		a.cleaner.Run(a.backgroundCtx)
	}()
	serveErrors := make(chan error, 1)
	go func() {
		err := a.server.Serve(a.listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErrors <- err
	}()
	a.logger.Info("HTTP server started", "address", a.listener.Addr().String())
	return serveErrors
}

func (a *App) Shutdown(ctx context.Context) error {
	var errs []error
	if err := a.server.Shutdown(ctx); err != nil {
		a.cancelRequests()
		_ = a.server.Close()
		errs = append(errs, fmt.Errorf("HTTP shutdown: %w", err))
	}

	a.stopBackground()
	if err := wait(ctx, a.backgroundDone); err != nil {
		errs = append(errs, fmt.Errorf("stop background workers: %w", err))
	}

	poolClosed := make(chan struct{})
	go func() {
		a.pool.Close()
		close(poolClosed)
	}()
	if err := wait(ctx, poolClosed); err != nil {
		errs = append(errs, fmt.Errorf("close database pool: %w", err))
	}

	if err := a.telemetry.Shutdown(ctx); err != nil {
		a.logger.Warn("telemetry shutdown failed", "error", err)
	}
	return errors.Join(errs...)
}

func wait(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutdown exceeded SHUTDOWN_TIMEOUT: %w", ctx.Err())
	}
}
