package save

import (
	"context"
	"fmt"
	"time"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/model"
)

type UseCase struct {
	trips     tripRepository
	positions positionRepository
	tx        txManager
	requests  requestCanceler
	metrics   positionMetrics
}

func New(trips tripRepository, positions positionRepository, tx txManager, requests requestCanceler, metrics positionMetrics) *UseCase {
	return &UseCase{trips: trips, positions: positions, tx: tx, requests: requests, metrics: metrics}
}

func (u *UseCase) Save(ctx context.Context, input model.SavePositionInput) (trip.Position, error) {
	position, err := trip.NewPosition(input.TripID, input.Coordinates, input.RecordedAt, time.Now())
	if err != nil {
		return trip.Position{}, err
	}
	var saved trip.Position
	err = u.tx.Do(ctx, func(ctx context.Context) error {
		if err := u.trips.TouchLastPosition(ctx, position.TripID, position.RecordedAt); err != nil {
			return err
		}
		saved, err = u.positions.Save(ctx, position)
		return err
	})
	if err != nil {
		return trip.Position{}, fmt.Errorf("save position: %w", err)
	}
	u.requests.Cancel(saved.TripID)
	u.metrics.PositionSaved(ctx)
	return saved, nil
}
