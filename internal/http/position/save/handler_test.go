package save_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/http/position/save"
	"github.com/KirillKrainov22/tripgo/internal/model"
)

const validBody = `{"coordinates":{"latitude":59.935,"longitude":30.335},"recorded_at":"2026-10-01T12:00:05Z"}`

func serve(usecase *MockpositionSaver, tripID uuid.UUID, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+tripID.String()+"/positions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	save.New(usecase, slog.New(slog.DiscardHandler)).Handle(recorder, request, tripID)
	return recorder
}

func problemCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var problem api.Problem
	if err := json.NewDecoder(recorder.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	return problem.Code
}

func TestSaveHandlerCreated(t *testing.T) {
	usecase := NewMockpositionSaver(gomock.NewController(t))
	tripID := uuid.New()
	recordedAt := time.Date(2026, 10, 1, 12, 0, 5, 0, time.UTC)
	usecase.EXPECT().Save(gomock.Any(), model.SavePositionInput{
		TripID:      tripID,
		Coordinates: trip.Coordinates{Latitude: 59.935, Longitude: 30.335},
		RecordedAt:  recordedAt,
	}).Return(trip.Position{ID: 42, TripID: tripID, Coordinates: trip.Coordinates{Latitude: 59.935, Longitude: 30.335}, RecordedAt: recordedAt}, nil)

	recorder := serve(usecase, tripID, validBody)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", recorder.Code)
	}
	var body api.TripPosition
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Id != 42 || body.TripId != tripID || !body.RecordedAt.Equal(recordedAt) {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestSaveHandlerInvalidBody(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "bad time", body: `{"coordinates":{"latitude":59.9,"longitude":30.3},"recorded_at":"yesterday"}`},
		{name: "missing coordinates", body: `{"recorded_at":"2026-10-01T12:00:05Z"}`},
		{name: "unknown field", body: `{"coordinates":{"latitude":59.9,"longitude":30.3},"recorded_at":"2026-10-01T12:00:05Z","speed":10}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usecase := NewMockpositionSaver(gomock.NewController(t))

			recorder := serve(usecase, uuid.New(), tt.body)

			if recorder.Code != http.StatusBadRequest || problemCode(t, recorder) != "invalid_request" {
				t.Fatalf("status = %d, want 400 invalid_request", recorder.Code)
			}
		})
	}
}

func TestSaveHandlerCompletedTrip(t *testing.T) {
	usecase := NewMockpositionSaver(gomock.NewController(t))
	usecase.EXPECT().Save(gomock.Any(), gomock.Any()).Return(trip.Position{}, trip.ErrTripCompleted)

	recorder := serve(usecase, uuid.New(), validBody)

	if recorder.Code != http.StatusConflict || problemCode(t, recorder) != "trip_completed" {
		t.Fatalf("status = %d, want 409 trip_completed", recorder.Code)
	}
}

func TestSaveHandlerUnknownTrip(t *testing.T) {
	usecase := NewMockpositionSaver(gomock.NewController(t))
	usecase.EXPECT().Save(gomock.Any(), gomock.Any()).Return(trip.Position{}, trip.ErrTripNotFound)

	recorder := serve(usecase, uuid.New(), validBody)

	if recorder.Code != http.StatusNotFound || problemCode(t, recorder) != "trip_not_found" {
		t.Fatalf("status = %d, want 404 trip_not_found", recorder.Code)
	}
}
