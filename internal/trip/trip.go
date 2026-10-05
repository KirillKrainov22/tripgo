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

type InvalidRequestError struct {
	Reason string
}

func InvalidRequest(format string, args ...any) error {
	return &InvalidRequestError{Reason: fmt.Sprintf(format, args...)}
}

func (e *InvalidRequestError) Error() string {
	return ErrInvalidRequest.Error() + ": " + e.Reason
}

func (e *InvalidRequestError) Unwrap() error {
	return ErrInvalidRequest
}

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
	if d.UserID == uuid.Nil {
		return InvalidRequest("user_id must be a nonzero UUID")
	}
	if d.DriverID == uuid.Nil {
		return InvalidRequest("driver_id must be a nonzero UUID")
	}
	if d.Price < 0 {
		return InvalidRequest("price must be nonnegative")
	}
	for _, point := range []struct {
		name  string
		value Coordinates
	}{{"start_point", d.StartPoint}, {"end_point", d.EndPoint}} {
		if !inRange(point.value.Latitude, -90, 90) {
			return InvalidRequest("%s.latitude must be between -90 and 90", point.name)
		}
		if !inRange(point.value.Longitude, -180, 180) {
			return InvalidRequest("%s.longitude must be between -180 and 180", point.name)
		}
	}
	return nil
}

func inRange(value, min, max float64) bool {
	return value >= min && value <= max
}
