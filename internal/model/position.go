package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

type SavePositionInput struct {
	TripID      uuid.UUID
	Coordinates trip.Coordinates
	RecordedAt  time.Time
}

type StaleTrip struct {
	TripID     uuid.UUID
	DriverID   uuid.UUID
	LastSeenAt time.Time
}
