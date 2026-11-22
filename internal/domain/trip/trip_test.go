package trip_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

func validData() trip.Data {
	return trip.Data{
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 59.9398, Longitude: 30.3146},
		EndPoint:   trip.Coordinates{Latitude: 59.929, Longitude: 30.3626},
		Price:      1450,
	}
}

func TestNew(t *testing.T) {
	id := uuid.New()
	data := validData()
	now := time.Date(2026, 10, 1, 15, 0, 0, 123456789, time.FixedZone("MSK", 3*60*60))

	value := trip.New(id, data, now)

	if value.ID != id || value.Data != data {
		t.Fatalf("unexpected trip: %+v", value)
	}
	if value.Status != trip.StatusActive {
		t.Fatalf("status = %q, want active", value.Status)
	}
	want := time.Date(2026, 10, 1, 12, 0, 0, 123456000, time.UTC)
	if !value.StartedAt.Equal(want) || value.StartedAt.Location() != time.UTC {
		t.Fatalf("started_at = %v, want %v", value.StartedAt, want)
	}
	if value.FinishedAt != nil {
		t.Fatalf("finished_at = %v, want nil", value.FinishedAt)
	}
}

func TestFinish(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC)
	earlier := at.Add(-time.Hour)
	tests := []struct {
		name       string
		value      trip.Trip
		wantErr    error
		wantStatus trip.Status
		wantAt     *time.Time
	}{
		{
			name:       "active trip becomes completed",
			value:      trip.Trip{Status: trip.StatusActive},
			wantStatus: trip.StatusCompleted,
			wantAt:     &at,
		},
		{
			name:       "completed trip cannot be finished again",
			value:      trip.Trip{Status: trip.StatusCompleted, FinishedAt: &earlier},
			wantErr:    trip.ErrTripCompleted,
			wantStatus: trip.StatusCompleted,
			wantAt:     &earlier,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := tt.value
			err := value.Finish(at)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if value.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", value.Status, tt.wantStatus)
			}
			if value.FinishedAt == nil || !value.FinishedAt.Equal(*tt.wantAt) {
				t.Fatalf("finished_at = %v, want %v", value.FinishedAt, *tt.wantAt)
			}
		})
	}
}

func TestDataValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(data *trip.Data)
		reason string
	}{
		{name: "valid data", change: func(*trip.Data) {}},
		{name: "zero user id", change: func(data *trip.Data) { data.UserID = uuid.Nil }, reason: "user_id must be a nonzero UUID"},
		{name: "zero driver id", change: func(data *trip.Data) { data.DriverID = uuid.Nil }, reason: "driver_id must be a nonzero UUID"},
		{name: "negative price", change: func(data *trip.Data) { data.Price = -1 }, reason: "price must be nonnegative"},
		{name: "zero price is allowed", change: func(data *trip.Data) { data.Price = 0 }},
		{name: "start latitude too big", change: func(data *trip.Data) { data.StartPoint.Latitude = 90.5 }, reason: "start_point.latitude must be between -90 and 90"},
		{name: "start longitude too small", change: func(data *trip.Data) { data.StartPoint.Longitude = -180.5 }, reason: "start_point.longitude must be between -180 and 180"},
		{name: "end latitude too small", change: func(data *trip.Data) { data.EndPoint.Latitude = -91 }, reason: "end_point.latitude must be between -90 and 90"},
		{name: "end longitude too big", change: func(data *trip.Data) { data.EndPoint.Longitude = 181 }, reason: "end_point.longitude must be between -180 and 180"},
		{name: "north pole is allowed", change: func(data *trip.Data) { data.StartPoint.Latitude = 90 }},
		{name: "south pole is allowed", change: func(data *trip.Data) { data.StartPoint.Latitude = -90 }},
		{name: "date line east is allowed", change: func(data *trip.Data) { data.StartPoint.Longitude = 180 }},
		{name: "date line west is allowed", change: func(data *trip.Data) { data.StartPoint.Longitude = -180 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := validData()
			tt.change(&data)
			err := data.Validate()
			if tt.reason == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var validation *trip.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %v, want ValidationError", err)
			}
			if validation.Reason != tt.reason {
				t.Fatalf("reason = %q, want %q", validation.Reason, tt.reason)
			}
		})
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := trip.NewValidationError("%s must be positive", "price")

	if err.Error() != "invalid request: price must be positive" {
		t.Fatalf("message = %q", err.Error())
	}
}
