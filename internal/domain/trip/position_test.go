package trip_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

func TestNewPosition(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	valid := trip.Coordinates{Latitude: 59.935, Longitude: 30.335}
	tests := []struct {
		name        string
		coordinates trip.Coordinates
		recordedAt  time.Time
		reason      string
	}{
		{name: "position from the past", coordinates: valid, recordedAt: now.Add(-time.Hour)},
		{name: "small clock skew is allowed", coordinates: valid, recordedAt: now.Add(59 * time.Second)},
		{name: "too far in the future", coordinates: valid, recordedAt: now.Add(2 * time.Minute), reason: "recorded_at must not be more than a minute in the future"},
		{name: "latitude out of range", coordinates: trip.Coordinates{Latitude: 91}, recordedAt: now, reason: "coordinates.latitude must be between -90 and 90"},
		{name: "longitude out of range", coordinates: trip.Coordinates{Longitude: -181}, recordedAt: now, reason: "coordinates.longitude must be between -180 and 180"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tripID := uuid.New()
			position, err := trip.NewPosition(tripID, tt.coordinates, tt.recordedAt, now)
			if tt.reason == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if position.TripID != tripID || !position.RecordedAt.Equal(tt.recordedAt) {
					t.Fatalf("unexpected position: %+v", position)
				}
				return
			}
			var validation *trip.ValidationError
			if !errors.As(err, &validation) || validation.Reason != tt.reason {
				t.Fatalf("error = %v, want reason %q", err, tt.reason)
			}
		})
	}
}
