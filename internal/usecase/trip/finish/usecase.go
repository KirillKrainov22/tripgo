package finish

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

type UseCase struct {
	trips   tripRepository
	tx      txManager
	metrics tripMetrics
	logger  *slog.Logger
}

func New(trips tripRepository, tx txManager, metrics tripMetrics, logger *slog.Logger) *UseCase {
	return &UseCase{trips: trips, tx: tx, metrics: metrics, logger: logger}
}

func (u *UseCase) Finish(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	var finished trip.Trip
	err := u.tx.Do(ctx, func(ctx context.Context) error {
		current, err := u.trips.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := current.Finish(time.Now()); err != nil {
			return err
		}
		if err := u.trips.SaveFinished(ctx, current); err != nil {
			return err
		}
		from := trip.StatusActive
		change := trip.StatusChange{TripID: id, From: &from, To: trip.StatusCompleted, At: *current.FinishedAt}
		if err := u.trips.RecordStatus(ctx, change); err != nil {
			return err
		}
		finished = current
		return nil
	})
	if err != nil {
		return trip.Trip{}, fmt.Errorf("finish trip: %w", err)
	}
	u.metrics.TripCompleted(ctx)
	u.logger.InfoContext(ctx, "trip completed", "trip_id", id.String())
	return finished, nil
}
