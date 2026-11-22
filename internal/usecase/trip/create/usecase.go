package create

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/model"
)

type UseCase struct {
	trips   tripRepository
	keys    idempotencyStore
	tx      txManager
	metrics tripMetrics
	logger  *slog.Logger
	keyTTL  time.Duration
}

func New(trips tripRepository, keys idempotencyStore, tx txManager, metrics tripMetrics, logger *slog.Logger, keyTTL time.Duration) *UseCase {
	return &UseCase{trips: trips, keys: keys, tx: tx, metrics: metrics, logger: logger, keyTTL: keyTTL}
}

func (u *UseCase) Create(ctx context.Context, input model.CreateTripInput) (model.CreateTripResult, error) {
	if err := input.Data.Validate(); err != nil {
		return model.CreateTripResult{}, err
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return model.CreateTripResult{}, fmt.Errorf("generate trip id: %w", err)
	}
	candidate := trip.New(id, input.Data, time.Now())

	var result model.CreateTripResult
	err = u.tx.Do(ctx, func(ctx context.Context) error {
		if input.IdempotencyKey != nil {
			stored, claimed, err := u.keys.Reserve(ctx, *input.IdempotencyKey, candidate, u.keyTTL)
			if err != nil {
				return err
			}
			if !claimed {
				if stored.Data != input.Data {
					return trip.ErrIdempotencyConflict
				}
				result = model.CreateTripResult{Trip: stored, Replayed: true}
				return nil
			}
		}
		created, err := u.trips.Create(ctx, candidate)
		if err != nil {
			return err
		}
		change := trip.StatusChange{TripID: created.ID, To: trip.StatusActive, At: created.StartedAt}
		if err := u.trips.RecordStatus(ctx, change); err != nil {
			return err
		}
		result = model.CreateTripResult{Trip: created}
		return nil
	})
	if err != nil {
		return model.CreateTripResult{}, fmt.Errorf("create trip: %w", err)
	}
	if !result.Replayed {
		u.metrics.TripCreated(ctx)
		u.logger.InfoContext(ctx, "trip created", "trip_id", result.Trip.ID.String())
	}
	return result, nil
}
