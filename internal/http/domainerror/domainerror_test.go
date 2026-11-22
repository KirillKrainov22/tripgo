package domainerror_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/http/domainerror"
)

func TestProblem(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantDetail string
	}{
		{
			name:       "validation error keeps its reason",
			err:        trip.NewValidationError("price must be nonnegative"),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
			wantDetail: "price must be nonnegative",
		},
		{name: "trip not found", err: trip.ErrTripNotFound, wantStatus: http.StatusNotFound, wantCode: "trip_not_found"},
		{name: "driver busy", err: trip.ErrDriverBusy, wantStatus: http.StatusConflict, wantCode: "driver_busy"},
		{name: "trip completed", err: trip.ErrTripCompleted, wantStatus: http.StatusConflict, wantCode: "trip_completed"},
		{name: "idempotency conflict", err: trip.ErrIdempotencyConflict, wantStatus: http.StatusConflict, wantCode: "idempotency_conflict"},
		{
			name:       "wrapped domain error",
			err:        fmt.Errorf("create trip: execute transaction: %w", trip.ErrDriverBusy),
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
		},
		{
			name:       "unknown error becomes internal error",
			err:        errors.New("dial tcp 10.0.0.5:5432: connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
			wantDetail: "An internal error occurred",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := domainerror.Problem(tt.err)
			if status != tt.wantStatus || int(body.Status) != tt.wantStatus {
				t.Fatalf("status = %d/%d, want %d", status, body.Status, tt.wantStatus)
			}
			if body.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", body.Code, tt.wantCode)
			}
			if body.Detail == nil {
				t.Fatal("detail is empty")
			}
			if tt.wantDetail != "" && *body.Detail != tt.wantDetail {
				t.Fatalf("detail = %q, want %q", *body.Detail, tt.wantDetail)
			}
			if strings.Contains(*body.Detail, "10.0.0.5") {
				t.Fatalf("internal details leaked: %q", *body.Detail)
			}
		})
	}
}
