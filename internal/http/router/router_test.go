package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/http/router"
	tripcreate "github.com/KirillKrainov22/tripgo/internal/http/trip/create"
	tripfinish "github.com/KirillKrainov22/tripgo/internal/http/trip/finish"
	tripget "github.com/KirillKrainov22/tripgo/internal/http/trip/get"
	"github.com/KirillKrainov22/tripgo/internal/model"
)

const internalErrorCode = "internal_error"

type fakeTrips struct{}

func (fakeTrips) Create(context.Context, model.CreateTripInput) (model.CreateTripResult, error) {
	return model.CreateTripResult{}, errors.New("not used")
}

func (fakeTrips) Get(context.Context, uuid.UUID) (trip.Trip, error) {
	return trip.Trip{}, trip.ErrTripNotFound
}

func (fakeTrips) Finish(context.Context, uuid.UUID) (trip.Trip, error) {
	panic("unexpected panic in handler")
}

type testServer struct {
	handler http.Handler
	spans   *tracetest.SpanRecorder
	metrics *sdkmetric.ManualReader
	logs    *bytes.Buffer
}

type traceLogHandler struct{ slog.Handler }

func (h traceLogHandler) Handle(ctx context.Context, record slog.Record) error {
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		record.AddAttrs(slog.String("trace_id", span.TraceID().String()), slog.String("span_id", span.SpanID().String()))
	}
	return h.Handler.Handle(ctx, record)
}

func newServer(t *testing.T, ping func(context.Context) error) testServer {
	t.Helper()
	logs := new(bytes.Buffer)
	logger := slog.New(traceLogHandler{slog.NewJSONHandler(logs, nil)})
	spans := tracetest.NewSpanRecorder()
	metrics := sdkmetric.NewManualReader()
	handler, err := router.New(router.Config{
		CreateTrip:     tripcreate.New(fakeTrips{}, logger),
		GetTrip:        tripget.New(fakeTrips{}, logger),
		FinishTrip:     tripfinish.New(fakeTrips{}, logger),
		Ping:           ping,
		Logger:         logger,
		TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)),
		Meter:          sdkmetric.NewMeterProvider(sdkmetric.WithReader(metrics)).Meter("test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return testServer{handler: handler, spans: spans, metrics: metrics, logs: logs}
}

func (s testServer) do(method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	s.handler.ServeHTTP(recorder, httptest.NewRequest(method, path, http.NoBody))
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

func okPing(context.Context) error {
	return nil
}

func TestHealthAndReady(t *testing.T) {
	server := newServer(t, okPing)
	if recorder := server.do(http.MethodGet, "/health"); recorder.Code != http.StatusOK {
		t.Fatalf("health status = %d", recorder.Code)
	}
	if recorder := server.do(http.MethodGet, "/ready"); recorder.Code != http.StatusOK {
		t.Fatalf("ready status = %d", recorder.Code)
	}

	broken := newServer(t, func(context.Context) error { return errors.New("database is down") })
	if recorder := broken.do(http.MethodGet, "/health"); recorder.Code != http.StatusOK {
		t.Fatalf("health must not depend on database, status = %d", recorder.Code)
	}
	if recorder := broken.do(http.MethodGet, "/ready"); recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status = %d, want 503", recorder.Code)
	}
}

func TestUnknownRouteAndBadParameter(t *testing.T) {
	server := newServer(t, okPing)

	recorder := server.do(http.MethodGet, "/api/v1/unknown")
	if recorder.Code != http.StatusNotFound || problemCode(t, recorder) != "route_not_found" {
		t.Fatalf("unexpected response for unknown route: %d", recorder.Code)
	}

	recorder = server.do(http.MethodGet, "/api/v1/trips/not-a-uuid")
	if recorder.Code != http.StatusBadRequest || problemCode(t, recorder) != "invalid_request" {
		t.Fatalf("unexpected response for bad id: %d", recorder.Code)
	}
}

func TestRecoverReturnsInternalError(t *testing.T) {
	server := newServer(t, okPing)

	recorder := server.do(http.MethodPost, "/api/v1/trips/"+uuid.NewString()+"/finish")

	if recorder.Code != http.StatusInternalServerError || problemCode(t, recorder) != internalErrorCode {
		t.Fatalf("unexpected response after panic: %d", recorder.Code)
	}
}

func TestRecoverRecordsFailedRequest(t *testing.T) {
	server := newServer(t, okPing)
	recorder := server.do(http.MethodPost, "/api/v1/trips/"+uuid.NewString()+"/finish")
	if recorder.Code != http.StatusInternalServerError || problemCode(t, recorder) != internalErrorCode {
		t.Fatalf("unexpected response after panic: %d", recorder.Code)
	}
	spans := server.spans.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", span.Status())
	}
	attrs := attribute.NewSet(span.Attributes()...)
	status, _ := attrs.Value("http.response.status_code")
	errorType, _ := attrs.Value("error.type")
	if status.AsInt64() != http.StatusInternalServerError || errorType.AsString() != internalErrorCode {
		t.Errorf("unexpected span attributes: %v", attrs.ToSlice())
	}
	lines := strings.Split(strings.TrimSpace(server.logs.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("log records = %d, want 1: %s", len(lines), server.logs.String())
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatal(err)
	}
	if record["msg"] != "request handled" || record["level"] != "ERROR" || record["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("unexpected request log: %v", record)
	}
	if record["trace_id"] != span.SpanContext().TraceID().String() || record["span_id"] != span.SpanContext().SpanID().String() {
		t.Errorf("request log is not correlated with its span: %v", record)
	}
	stack, _ := record["stack"].(string)
	if !strings.Contains(stack, "fakeTrips.Finish") || record["panic"] != "unexpected panic in handler" {
		t.Errorf("request log has no panic details: %v", record)
	}
	assertPanicMetrics(t, server.metrics)
}

func assertPanicMetrics(t *testing.T, reader *sdkmetric.ManualReader) {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	foundDuration, foundActive := false, false
	for _, scope := range data.ScopeMetrics {
		for _, item := range scope.Metrics {
			switch item.Name {
			case "http.server.active_requests":
				foundActive = true
				sum, ok := item.Data.(metricdata.Sum[int64])
				if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 0 {
					t.Errorf("active requests must return to zero: %+v", item.Data)
				}
			case "http.server.request.duration":
				foundDuration = true
				histogram, ok := item.Data.(metricdata.Histogram[float64])
				if !ok || len(histogram.DataPoints) != 1 {
					t.Errorf("unexpected duration: %+v", item.Data)
					continue
				}
				point := histogram.DataPoints[0]
				status, _ := point.Attributes.Value("http.response.status_code")
				if point.Count != 1 || status.AsInt64() != http.StatusInternalServerError {
					t.Errorf("unexpected duration point: %+v", point)
				}
			}
		}
	}
	if !foundDuration || !foundActive {
		t.Errorf("missing request metrics: duration=%t active=%t", foundDuration, foundActive)
	}
}

func TestTelemetryUsesRouteTemplate(t *testing.T) {
	server := newServer(t, okPing)
	id := uuid.NewString()

	server.do(http.MethodGet, "/api/v1/trips/"+id)

	spans := server.spans.Ended()
	if len(spans) != 1 || spans[0].Name() != "GET /api/v1/trips/{tripId}" {
		t.Fatalf("unexpected spans: %v", spans)
	}

	var data metricdata.ResourceMetrics
	if err := server.metrics.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, scope := range data.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "http.server.request.duration" {
				continue
			}
			histogram, ok := item.Data.(metricdata.Histogram[float64])
			if !ok || len(histogram.DataPoints) != 1 {
				t.Fatalf("unexpected histogram: %+v", item.Data)
			}
			point := histogram.DataPoints[0]
			route, _ := point.Attributes.Value(attribute.Key("http.route"))
			status, _ := point.Attributes.Value(attribute.Key("http.response.status_code"))
			if route.AsString() != "/api/v1/trips/{tripId}" || status.AsInt64() != http.StatusNotFound {
				t.Fatalf("unexpected attributes: %v", point.Attributes.ToSlice())
			}
			found = true
		}
	}
	if !found {
		t.Fatal("http.server.request.duration was not recorded")
	}
}
