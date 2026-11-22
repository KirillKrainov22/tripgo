package list_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/http/position/list"
)

func serve(usecase *MockpositionLister, tripID uuid.UUID) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID.String()+"/positions", http.NoBody)
	recorder := httptest.NewRecorder()
	list.New(usecase, slog.New(slog.DiscardHandler)).Handle(recorder, request, tripID)
	return recorder
}

func TestListHandlerReturnsRoute(t *testing.T) {
	usecase := NewMockpositionLister(gomock.NewController(t))
	tripID := uuid.New()
	first := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	usecase.EXPECT().List(gomock.Any(), tripID).Return([]trip.Position{
		{ID: 1, TripID: tripID, RecordedAt: first},
		{ID: 2, TripID: tripID, RecordedAt: first.Add(time.Minute)},
	}, nil)

	recorder := serve(usecase, tripID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body api.TripPositionsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Positions) != 2 || body.Positions[0].Id != 1 || body.Positions[1].Id != 2 {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestListHandlerEmptyRoute(t *testing.T) {
	usecase := NewMockpositionLister(gomock.NewController(t))
	tripID := uuid.New()
	usecase.EXPECT().List(gomock.Any(), tripID).Return([]trip.Position{}, nil)

	recorder := serve(usecase, tripID)

	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"positions\":[]}\n" {
		t.Fatalf("unexpected response: %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestListHandlerUnknownTrip(t *testing.T) {
	usecase := NewMockpositionLister(gomock.NewController(t))
	tripID := uuid.New()
	usecase.EXPECT().List(gomock.Any(), tripID).Return(nil, trip.ErrTripNotFound)

	recorder := serve(usecase, tripID)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}
