//go:generate go tool mockgen -source=${GOFILE} -destination=mocks_test.go -package=${GOPACKAGE}_test
package save

import (
	"context"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/model"
)

type positionSaver interface {
	Save(ctx context.Context, input model.SavePositionInput) (trip.Position, error)
}
