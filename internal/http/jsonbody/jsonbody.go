package jsonbody

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

const maxBytes = 1 << 20

type Field struct {
	Path     string
	Raw      json.RawMessage
	Expected string
	Dest     any
}

func Read(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return nil, trip.NewValidationError("Content-Type must be application/json")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		return nil, trip.NewValidationError("request body cannot be read or exceeds 1 MiB")
	}
	return body, nil
}

func RequireEmpty(w http.ResponseWriter, r *http.Request) error {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil || len(bytes.TrimSpace(body)) != 0 {
		return trip.NewValidationError("request body must be empty")
	}
	return nil
}

func Object(body []byte, name string, required ...string) (map[string]json.RawMessage, error) {
	object, prefix := "request body", ""
	if name != "" {
		object, prefix = name, name+"."
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, trip.NewValidationError("%s must be a JSON object", object)
	}
	for _, field := range required {
		value, exists := fields[field]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, trip.NewValidationError("%s%s is required", prefix, field)
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
		return nil, trip.NewValidationError("unknown fields: %s", strings.Join(unknown, ", "))
	}
	return fields, nil
}

func Decode(fields []Field) error {
	for _, field := range fields {
		if err := json.Unmarshal(field.Raw, field.Dest); err != nil {
			return trip.NewValidationError("%s must be %s", field.Path, field.Expected)
		}
	}
	return nil
}

func Coordinates(fields map[string]json.RawMessage, name string, dest *trip.Coordinates) ([]Field, error) {
	values, err := Object(fields[name], name, "latitude", "longitude")
	if err != nil {
		return nil, err
	}
	return []Field{
		{Path: name + ".latitude", Raw: values["latitude"], Expected: "a number", Dest: &dest.Latitude},
		{Path: name + ".longitude", Raw: values["longitude"], Expected: "a number", Dest: &dest.Longitude},
	}, nil
}
