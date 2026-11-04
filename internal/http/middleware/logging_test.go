package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/http/middleware"
	"github.com/KirillKrainov22/tripgo/internal/http/respond"
)

func serveWithError(t *testing.T, err error) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	routes := chi.NewRouter()
	routes.Get("/api/v1/trips/{tripId}", func(w http.ResponseWriter, r *http.Request) {
		respond.Error(w, r, logger, err)
	})
	handler := middleware.Logging(logger, routes)(routes)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+uuid.NewString(), http.NoBody))

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

func TestLoggingWritesOneRecordForInternalError(t *testing.T) {
	records := serveWithError(t, errors.New("dial tcp: connection refused"))

	if len(records) != 1 {
		t.Fatalf("records = %d, want 1: %v", len(records), records)
	}
	record := records[0]
	if record["level"] != "ERROR" || record["msg"] != "request handled" {
		t.Fatalf("unexpected record: %v", record)
	}
	if record["route"] != "/api/v1/trips/{tripId}" || record["status"] != float64(http.StatusInternalServerError) {
		t.Fatalf("unexpected route or status: %v", record)
	}
	if record["error"] != "dial tcp: connection refused" {
		t.Fatalf("error = %v", record["error"])
	}
}

func TestLoggingUsesInfoForClientError(t *testing.T) {
	records := serveWithError(t, trip.ErrTripNotFound)

	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0]["level"] != "INFO" || records[0]["status"] != float64(http.StatusNotFound) {
		t.Fatalf("unexpected record: %v", records[0])
	}
	if _, ok := records[0]["error"]; ok {
		t.Fatalf("client error must not be logged as failure: %v", records[0])
	}
}
