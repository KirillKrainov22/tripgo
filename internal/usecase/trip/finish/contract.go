//go:generate go tool mockgen -source=${GOFILE} -destination=mocks_test.go -package=${GOPACKAGE}_test
package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

type (
	tripRepository interface {
		GetForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error)
		SaveFinished(ctx context.Context, value trip.Trip) error
		RecordStatus(ctx context.Context, change trip.StatusChange) error
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	tripMetrics interface {
		TripCompleted(ctx context.Context)
	}
)
