package trip

import (
	"time"

	"github.com/google/uuid"
)

const maxFutureSkew = time.Minute

type Position struct {
	ID          int64
	TripID      uuid.UUID
	Coordinates Coordinates
	RecordedAt  time.Time
}

func NewPosition(tripID uuid.UUID, coordinates Coordinates, recordedAt, now time.Time) (Position, error) {
	if err := coordinates.Validate("coordinates"); err != nil {
		return Position{}, err
	}
	if recordedAt.After(now.Add(maxFutureSkew)) {
		return Position{}, NewValidationError("recorded_at must not be more than a minute in the future")
	}
	return Position{TripID: tripID, Coordinates: coordinates, RecordedAt: Timestamp(recordedAt)}, nil
}
