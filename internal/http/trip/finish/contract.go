//go:generate go tool mockgen -source=${GOFILE} -destination=mocks_test.go -package=${GOPACKAGE}_test
package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

type tripFinisher interface {
	Finish(ctx context.Context, id uuid.UUID) (trip.Trip, error)
}
