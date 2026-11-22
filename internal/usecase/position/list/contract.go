//go:generate go tool mockgen -source=${GOFILE} -destination=mocks_test.go -package=${GOPACKAGE}_test
package list

import (
	"context"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

type (
	tripRepository interface {
		Get(ctx context.Context, id uuid.UUID) (trip.Trip, error)
	}

	positionRepository interface {
		List(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error)
	}
)
