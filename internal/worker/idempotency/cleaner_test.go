package idempotency_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/KirillKrainov22/tripgo/internal/worker/idempotency"
)

func TestCleanerStopsWhenContextCanceled(t *testing.T) {
	keys := NewMockkeyRemover(gomock.NewController(t))
	ctx, cancel := context.WithCancel(context.Background())
	keys.EXPECT().DeleteExpired(gomock.Any()).DoAndReturn(func(context.Context) (int64, error) {
		cancel()
		return 3, nil
	})

	idempotency.NewCleaner(keys, time.Hour, slog.New(slog.DiscardHandler)).Run(ctx)
}

func TestCleanerSurvivesRepositoryError(t *testing.T) {
	keys := NewMockkeyRemover(gomock.NewController(t))
	ctx, cancel := context.WithCancel(context.Background())
	keys.EXPECT().DeleteExpired(gomock.Any()).DoAndReturn(func(context.Context) (int64, error) {
		cancel()
		return 0, errors.New("database is unavailable")
	})

	idempotency.NewCleaner(keys, time.Hour, slog.New(slog.DiscardHandler)).Run(ctx)
}
