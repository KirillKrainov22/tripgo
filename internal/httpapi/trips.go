package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/trip"
)

const maxRequestBytes = 1 << 20

func (h *handler) CreateTrip(w http.ResponseWriter, r *http.Request, _ api.CreateTripParams) {
	data, err := decodeTripData(w, r)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	value, err := h.trips.Create(r.Context(), data)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/trips/"+value.ID.String())
	h.writeJSON(w, http.StatusCreated, toAPITrip(value))
}

func (h *handler) GetTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	value, err := h.trips.Get(r.Context(), id)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, toAPITrip(value))
}

func (h *handler) FinishTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil || len(bytes.TrimSpace(body)) != 0 {
		h.handleError(w, r, trip.ErrInvalidRequest)
		return
	}
	value, err := h.trips.Finish(r.Context(), id)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, toAPITrip(value))
}

func decodeTripData(w http.ResponseWriter, r *http.Request) (trip.Data, error) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return trip.Data{}, trip.ErrInvalidRequest
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		return trip.Data{}, trip.ErrInvalidRequest
	}
	fields, err := exactObject(body, "user_id", "driver_id", "start_point", "end_point", "price")
	if err != nil {
		return trip.Data{}, err
	}
	for _, field := range []string{"start_point", "end_point"} {
		if _, err := exactObject(fields[field], "latitude", "longitude"); err != nil {
			return trip.Data{}, err
		}
	}
	var input api.CreateTripJSONRequestBody
	if err := json.Unmarshal(body, &input); err != nil {
		return trip.Data{}, trip.ErrInvalidRequest
	}
	data := trip.Data{
		UserID:     input.UserId,
		DriverID:   input.DriverId,
		Price:      input.Price,
		StartPoint: trip.Coordinates{Latitude: input.StartPoint.Latitude, Longitude: input.StartPoint.Longitude},
		EndPoint:   trip.Coordinates{Latitude: input.EndPoint.Latitude, Longitude: input.EndPoint.Longitude},
	}
	return data, data.Validate()
}

func exactObject(body []byte, required ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || len(fields) != len(required) {
		return nil, trip.ErrInvalidRequest
	}
	for _, name := range required {
		value, exists := fields[name]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, trip.ErrInvalidRequest
		}
	}
	return fields, nil
}

func toAPITrip(value trip.Trip) api.Trip {
	return api.Trip{
		Id:         value.ID,
		UserId:     value.UserID,
		DriverId:   value.DriverID,
		StartPoint: api.Coordinates{Latitude: value.StartPoint.Latitude, Longitude: value.StartPoint.Longitude},
		EndPoint:   api.Coordinates{Latitude: value.EndPoint.Latitude, Longitude: value.EndPoint.Longitude},
		Price:      value.Price,
		Status:     api.TripStatus(value.Status),
		StartedAt:  value.StartedAt,
		FinishedAt: value.FinishedAt,
	}
}

func (h *handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, trip.ErrInvalidRequest):
		h.problem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "The request does not match the API contract")
	case errors.Is(err, trip.ErrNotFound):
		h.problem(w, r, http.StatusNotFound, "trip_not_found", "Trip not found", "The requested trip does not exist")
	case errors.Is(err, trip.ErrDriverBusy):
		h.problem(w, r, http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.Is(err, trip.ErrCompleted):
		h.problem(w, r, http.StatusConflict, "trip_completed", "Trip completed", "The trip is already completed")
	default:
		h.logger.Error("trip request failed", "method", r.Method, "error", err)
		h.problem(w, r, http.StatusInternalServerError, "internal_error", "Internal error", "An internal error occurred")
	}
}

func (h *handler) problem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	instance := r.URL.Path
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(api.Problem{
		Type:     "https://tripgo.example/problems/" + strings.ReplaceAll(code, "_", "-"),
		Title:    title,
		Status:   int32(status),
		Code:     code,
		Detail:   &detail,
		Instance: &instance,
	})
	if err != nil {
		h.logger.Error("write problem response", "error", err)
	}
}

func (h *handler) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.Error("write JSON response", "error", err)
	}
}
