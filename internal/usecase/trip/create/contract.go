//go:generate go tool mockgen -source=${GOFILE} -destination=mocks_test.go -package=${GOPACKAGE}_test
package create

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

type (
	tripRepository interface {
		Create(ctx context.Context, value trip.Trip) (trip.Trip, error)
		RecordStatus(ctx context.Context, change trip.StatusChange) error
	}

	idempotencyStore interface {
		Reserve(ctx context.Context, key uuid.UUID, candidate trip.Trip, ttl time.Duration) (trip.Trip, bool, error)
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	tripMetrics interface {
		TripCreated(ctx context.Context)
	}
)
