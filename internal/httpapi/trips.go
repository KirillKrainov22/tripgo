package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"

	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/trip"
)

const maxRequestBytes = 1 << 20

func (h *handler) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	data, err := decodeTripData(w, r)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	result, err := h.trips.Create(r.Context(), data, params.IdempotencyKey)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	w.Header().Set("Location", "/api/v1/trips/"+result.Trip.ID.String())
	h.writeJSON(w, status, toAPITrip(result.Trip))
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
		h.handleError(w, r, trip.InvalidRequest("request body must be empty"))
		return
	}
	value, err := h.trips.Finish(r.Context(), id)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, toAPITrip(value))
}

type jsonField struct {
	path     string
	raw      json.RawMessage
	expected string
	dest     any
}

func decodeTripData(w http.ResponseWriter, r *http.Request) (trip.Data, error) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return trip.Data{}, trip.InvalidRequest("Content-Type must be application/json")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		return trip.Data{}, trip.InvalidRequest("request body cannot be read or exceeds 1 MiB")
	}
	fields, err := exactObject(body, "", "user_id", "driver_id", "start_point", "end_point", "price")
	if err != nil {
		return trip.Data{}, err
	}
	var data trip.Data
	values := []jsonField{
		{"user_id", fields["user_id"], "a UUID", &data.UserID},
		{"driver_id", fields["driver_id"], "a UUID", &data.DriverID},
		{"price", fields["price"], "an integer", &data.Price},
	}
	for _, point := range []struct {
		name string
		dest *trip.Coordinates
	}{{"start_point", &data.StartPoint}, {"end_point", &data.EndPoint}} {
		coordinates, err := exactObject(fields[point.name], point.name, "latitude", "longitude")
		if err != nil {
			return trip.Data{}, err
		}
		values = append(values,
			jsonField{point.name + ".latitude", coordinates["latitude"], "a number", &point.dest.Latitude},
			jsonField{point.name + ".longitude", coordinates["longitude"], "a number", &point.dest.Longitude},
		)
	}
	for _, value := range values {
		if err := json.Unmarshal(value.raw, value.dest); err != nil {
			return trip.Data{}, trip.InvalidRequest("%s must be %s", value.path, value.expected)
		}
	}
	return data, data.Validate()
}

func exactObject(body []byte, name string, required ...string) (map[string]json.RawMessage, error) {
	object, prefix := "request body", ""
	if name != "" {
		object, prefix = name, name+"."
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, trip.InvalidRequest("%s must be a JSON object", object)
	}
	for _, field := range required {
		value, exists := fields[field]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, trip.InvalidRequest("%s%s is required", prefix, field)
		}
	}
	var unknown []string
	for field := range fields {
		if !slices.Contains(required, field) {
			unknown = append(unknown, prefix+field)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return nil, trip.InvalidRequest("unknown fields: %s", strings.Join(unknown, ", "))
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
		detail := "The request does not match the API contract"
		var invalid *trip.InvalidRequestError
		if errors.As(err, &invalid) {
			detail = invalid.Reason
		}
		h.problem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", detail)
	case errors.Is(err, trip.ErrNotFound):
		h.problem(w, r, http.StatusNotFound, "trip_not_found", "Trip not found", "The requested trip does not exist")
	case errors.Is(err, trip.ErrDriverBusy):
		h.problem(w, r, http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.Is(err, trip.ErrCompleted):
		h.problem(w, r, http.StatusConflict, "trip_completed", "Trip completed", "The trip is already completed")
	case errors.Is(err, trip.ErrIdempotencyConflict):
		h.problem(w, r, http.StatusConflict, "idempotency_conflict", "Idempotency conflict", "Idempotency-Key was already used with a different request body")
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
