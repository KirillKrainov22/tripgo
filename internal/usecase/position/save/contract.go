//go:generate go tool mockgen -source=${GOFILE} -destination=mocks_test.go -package=${GOPACKAGE}_test
package save

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

type (
	tripRepository interface {
		TouchLastPosition(ctx context.Context, id uuid.UUID, recordedAt time.Time) error
	}

	positionRepository interface {
		Save(ctx context.Context, value trip.Position) (trip.Position, error)
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	requestCanceler interface {
		Cancel(tripID uuid.UUID)
	}

	positionMetrics interface {
		PositionSaved(ctx context.Context)
	}
)
