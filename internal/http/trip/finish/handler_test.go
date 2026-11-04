package finish_test

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
	"github.com/KirillKrainov22/tripgo/internal/http/trip/finish"
)

func serve(usecase *MocktripFinisher, id uuid.UUID, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	finish.New(usecase, slog.New(slog.DiscardHandler)).Handle(recorder, request, id)
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

func TestFinishHandlerSuccess(t *testing.T) {
	usecase := NewMocktripFinisher(gomock.NewController(t))
	value := trip.New(uuid.New(), trip.Data{UserID: uuid.New(), DriverID: uuid.New()}, time.Now().Add(-time.Hour))
	if err := value.Finish(time.Now()); err != nil {
		t.Fatal(err)
	}
	usecase.EXPECT().Finish(gomock.Any(), value.ID).Return(value, nil)

	recorder := serve(usecase, value.ID, "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body api.Trip
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != api.Completed || body.FinishedAt == nil {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestFinishHandlerCompletedTrip(t *testing.T) {
	usecase := NewMocktripFinisher(gomock.NewController(t))
	id := uuid.New()
	usecase.EXPECT().Finish(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripCompleted)

	recorder := serve(usecase, id, "")

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	if code := problemCode(t, recorder); code != "trip_completed" {
		t.Fatalf("code = %q, want trip_completed", code)
	}
}

func TestFinishHandlerRejectsBody(t *testing.T) {
	usecase := NewMocktripFinisher(gomock.NewController(t))

	recorder := serve(usecase, uuid.New(), `{"a":1}`)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if code := problemCode(t, recorder); code != "invalid_request" {
		t.Fatalf("code = %q, want invalid_request", code)
	}
}
