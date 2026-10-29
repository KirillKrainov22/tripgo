package model

import (
	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

type CreateTripInput struct {
	Data           trip.Data
	IdempotencyKey *uuid.UUID
}

type CreateTripResult struct {
	Trip     trip.Trip
	Replayed bool
}
