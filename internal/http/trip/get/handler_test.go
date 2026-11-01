package get_test

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
	"github.com/KirillKrainov22/tripgo/internal/http/trip/get"
)

func serve(usecase *MocktripGetter, id uuid.UUID) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), http.NoBody)
	recorder := httptest.NewRecorder()
	get.New(usecase, slog.New(slog.DiscardHandler)).Handle(recorder, request, id)
	return recorder
}

func TestGetHandlerSuccess(t *testing.T) {
	usecase := NewMocktripGetter(gomock.NewController(t))
	stored := trip.New(uuid.New(), trip.Data{UserID: uuid.New(), DriverID: uuid.New(), Price: 300}, time.Now())
	usecase.EXPECT().Get(gomock.Any(), stored.ID).Return(stored, nil)

	recorder := serve(usecase, stored.ID)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body api.Trip
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Id != stored.ID || body.Price != 300 {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestGetHandlerNotFound(t *testing.T) {
	usecase := NewMocktripGetter(gomock.NewController(t))
	id := uuid.New()
	usecase.EXPECT().Get(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)

	recorder := serve(usecase, id)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	var problem api.Problem
	if err := json.NewDecoder(recorder.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "trip_not_found" {
		t.Fatalf("code = %q, want trip_not_found", problem.Code)
	}
}
