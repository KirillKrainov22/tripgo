package domainerror

import (
	"errors"
	"net/http"
	"strings"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	api "github.com/KirillKrainov22/tripgo/internal/generated"
)

func Problem(err error) (status int, body api.Problem) {
	var validation *trip.ValidationError
	switch {
	case errors.As(err, &validation):
		return New(http.StatusBadRequest, "invalid_request", "Invalid request", validation.Reason)
	case errors.Is(err, trip.ErrTripNotFound):
		return New(http.StatusNotFound, "trip_not_found", "Trip not found", "The requested trip does not exist")
	case errors.Is(err, trip.ErrDriverBusy):
		return New(http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.Is(err, trip.ErrTripCompleted):
		return New(http.StatusConflict, "trip_completed", "Trip completed", "The trip is already completed")
	case errors.Is(err, trip.ErrIdempotencyConflict):
		return New(http.StatusConflict, "idempotency_conflict", "Idempotency conflict", "Idempotency-Key was already used with a different request body")
	default:
		return New(http.StatusInternalServerError, "internal_error", "Internal error", "An internal error occurred")
	}
}

func New(status int32, code, title, detail string) (int, api.Problem) {
	return int(status), api.Problem{
		Type:   "https://tripgo.example/problems/" + strings.ReplaceAll(code, "_", "-"),
		Title:  title,
		Status: status,
		Code:   code,
		Detail: &detail,
	}
}
