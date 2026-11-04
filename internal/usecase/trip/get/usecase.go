package get

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

type UseCase struct {
	trips tripRepository
}

func New(trips tripRepository) *UseCase {
	return &UseCase{trips: trips}
}

func (u *UseCase) Get(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	value, err := u.trips.Get(ctx, id)
	if err != nil {
		return trip.Trip{}, fmt.Errorf("get trip: %w", err)
	}
	return value, nil
}
