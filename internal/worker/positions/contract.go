//go:generate go tool mockgen -source=${GOFILE} -destination=mocks_test.go -package=${GOPACKAGE}_test
package positions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/model"
)

type (
	staleTripFinder interface {
		FindStale(ctx context.Context, staleAfter time.Duration) ([]model.StaleTrip, error)
	}

	pushGateway interface {
		RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error
	}
)
