package create_test

import (
	"encoding/json"
	"errors"
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
	"github.com/KirillKrainov22/tripgo/internal/http/trip/create"
	"github.com/KirillKrainov22/tripgo/internal/model"
)

const validBody = `{"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c","driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5",` +
	`"start_point":{"latitude":59.9398,"longitude":30.3146},"end_point":{"latitude":59.929,"longitude":30.3626},"price":1450}`

func serve(t *testing.T, usecase *MocktripCreator, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	create.New(usecase, slog.New(slog.DiscardHandler)).Handle(recorder, request, api.CreateTripParams{})
	return recorder
}

func decodeProblem(t *testing.T, recorder *httptest.ResponseRecorder) api.Problem {
	t.Helper()
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/problem+json" {
		t.Fatalf("content type = %q", contentType)
	}
	var problem api.Problem
	if err := json.NewDecoder(recorder.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	return problem
}

func TestCreateHandlerSuccess(t *testing.T) {
	usecase := NewMocktripCreator(gomock.NewController(t))
	created := trip.New(uuid.New(), trip.Data{
		UserID:   uuid.MustParse("5cb72c04-7650-45c9-a79b-bcdba0631e0c"),
		DriverID: uuid.MustParse("8860b315-ec86-42eb-a17c-7c163d721ff5"),
		Price:    1450,
	}, time.Now())
	usecase.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, input model.CreateTripInput) (model.CreateTripResult, error) {
			if input.Data.Price != 1450 || input.Data.StartPoint.Latitude != 59.9398 {
				t.Fatalf("unexpected input: %+v", input)
			}
			return model.CreateTripResult{Trip: created}, nil
		})

	recorder := serve(t, usecase, validBody)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/api/v1/trips/"+created.ID.String() {
		t.Fatalf("location = %q", location)
	}
	var body api.Trip
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Id != created.ID || body.Status != api.Active {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestCreateHandlerReplay(t *testing.T) {
	usecase := NewMocktripCreator(gomock.NewController(t))
	usecase.EXPECT().Create(gomock.Any(), gomock.Any()).
		Return(model.CreateTripResult{Trip: trip.New(uuid.New(), trip.Data{}, time.Now()), Replayed: true}, nil)

	recorder := serve(t, usecase, validBody)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestCreateHandlerInvalidBody(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		detail string
	}{
		{name: "not json", body: `hello`, detail: "request body must be a JSON object"},
		{name: "unknown field", body: strings.Replace(validBody, `"price":1450`, `"price":1450,"comment":"x"`, 1), detail: "unknown fields: comment"},
		{name: "missing field", body: strings.Replace(validBody, `,"price":1450`, ``, 1), detail: "price is required"},
		{name: "bad uuid", body: strings.Replace(validBody, `"5cb72c04-7650-45c9-a79b-bcdba0631e0c"`, `"abc"`, 1), detail: "user_id must be a UUID"},
		{name: "latitude out of range", body: strings.Replace(validBody, `"latitude":59.9398`, `"latitude":95`, 1), detail: "start_point.latitude must be between -90 and 90"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usecase := NewMocktripCreator(gomock.NewController(t))

			recorder := serve(t, usecase, tt.body)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", recorder.Code)
			}
			problem := decodeProblem(t, recorder)
			if problem.Code != "invalid_request" || problem.Detail == nil || *problem.Detail != tt.detail {
				t.Fatalf("unexpected problem: %+v", problem)
			}
		})
	}
}

func TestCreateHandlerDriverBusy(t *testing.T) {
	usecase := NewMocktripCreator(gomock.NewController(t))
	usecase.EXPECT().Create(gomock.Any(), gomock.Any()).Return(model.CreateTripResult{}, trip.ErrDriverBusy)

	recorder := serve(t, usecase, validBody)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	if problem := decodeProblem(t, recorder); problem.Code != "driver_busy" {
		t.Fatalf("code = %q, want driver_busy", problem.Code)
	}
}

func TestCreateHandlerInternalErrorHidesDetails(t *testing.T) {
	usecase := NewMocktripCreator(gomock.NewController(t))
	usecase.EXPECT().Create(gomock.Any(), gomock.Any()).
		Return(model.CreateTripResult{}, errors.New(`insert trip: ERROR: relation "trips" does not exist`))

	recorder := serve(t, usecase, validBody)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "relation") || strings.Contains(body, "insert trip") {
		t.Fatalf("internal details leaked: %s", body)
	}
	var problem api.Problem
	if err := json.Unmarshal([]byte(body), &problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != "internal_error" {
		t.Fatalf("code = %q, want internal_error", problem.Code)
	}
}
