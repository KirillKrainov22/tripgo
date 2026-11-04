package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/KirillKrainov22/tripgo/internal/config"
	"github.com/KirillKrainov22/tripgo/internal/container"
)

func main() {
	os.Exit(start())
}

func start() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "service stopped with an error:", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, cfg config.Config) error {
	app, err := container.New(ctx, cfg)
	if err != nil {
		return err
	}
	serveErrors := app.Start()

	var serveErr error
	select {
	case <-ctx.Done():
		app.Logger().Info("shutdown requested")
	case serveErr = <-serveErrors:
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	if err := app.Shutdown(shutdownCtx); err != nil {
		app.Logger().Error("service shutdown failed", "error", err)
		return errors.Join(serveErr, err)
	}
	if serveErr != nil {
		return fmt.Errorf("serve HTTP: %w", serveErr)
	}
	app.Logger().Info("service stopped")
	return nil
}
