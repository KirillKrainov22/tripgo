//go:generate go tool mockgen -source=${GOFILE} -destination=mocks_test.go -package=${GOPACKAGE}_test
package create

import (
	"context"

	"github.com/KirillKrainov22/tripgo/internal/model"
)

type tripCreator interface {
	Create(ctx context.Context, input model.CreateTripInput) (model.CreateTripResult, error)
}
