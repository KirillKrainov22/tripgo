package trip

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidRequest      = errors.New("invalid request")
	ErrNotFound            = errors.New("trip not found")
	ErrDriverBusy          = errors.New("driver already has an active trip")
	ErrCompleted           = errors.New("trip is already completed")
	ErrIdempotencyConflict = errors.New("idempotency key was used with different trip data")
)

const (
	Active    = "active"
	Completed = "completed"
)

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

type Data struct {
	UserID     uuid.UUID
	DriverID   uuid.UUID
	StartPoint Coordinates
	EndPoint   Coordinates
	Price      int64
}

type Trip struct {
	Data
	ID         uuid.UUID
	Status     string
	StartedAt  time.Time
	FinishedAt *time.Time
}

func (d Data) Validate() error {
	if d.UserID == uuid.Nil || d.DriverID == uuid.Nil {
		return fmt.Errorf("%w: user_id and driver_id must be nonzero UUIDs", ErrInvalidRequest)
	}
	if d.Price < 0 {
		return fmt.Errorf("%w: price must be nonnegative", ErrInvalidRequest)
	}
	for _, point := range []Coordinates{d.StartPoint, d.EndPoint} {
		if !inRange(point.Latitude, -90, 90) || !inRange(point.Longitude, -180, 180) {
			return fmt.Errorf("%w: coordinates are outside their valid ranges", ErrInvalidRequest)
		}
	}
	return nil
}

func inRange(value, min, max float64) bool {
	return value >= min && value <= max
}
