package trip

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
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
	Status     Status
	StartedAt  time.Time
	FinishedAt *time.Time
}

type StatusChange struct {
	TripID uuid.UUID
	From   *Status
	To     Status
	At     time.Time
}

func New(id uuid.UUID, data Data, now time.Time) Trip {
	return Trip{
		Data:      data,
		ID:        id,
		Status:    StatusActive,
		StartedAt: Timestamp(now),
	}
}

func (t *Trip) Finish(at time.Time) error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}
	finishedAt := Timestamp(at)
	t.Status = StatusCompleted
	t.FinishedAt = &finishedAt
	return nil
}

func (d Data) Validate() error {
	if d.UserID == uuid.Nil {
		return NewValidationError("user_id must be a nonzero UUID")
	}
	if d.DriverID == uuid.Nil {
		return NewValidationError("driver_id must be a nonzero UUID")
	}
	if d.Price < 0 {
		return NewValidationError("price must be nonnegative")
	}
	if err := d.StartPoint.Validate("start_point"); err != nil {
		return err
	}
	return d.EndPoint.Validate("end_point")
}

func (c Coordinates) Validate(name string) error {
	if c.Latitude < -90 || c.Latitude > 90 {
		return NewValidationError("%s.latitude must be between -90 and 90", name)
	}
	if c.Longitude < -180 || c.Longitude > 180 {
		return NewValidationError("%s.longitude must be between -180 and 180", name)
	}
	return nil
}

func Timestamp(t time.Time) time.Time {
	return t.UTC().Truncate(time.Microsecond)
}
