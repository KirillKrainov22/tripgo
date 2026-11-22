package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/KirillKrainov22/tripgo/internal/http/middleware"
)

type recoveryServer struct {
	handler http.Handler
	metrics *sdkmetric.ManualReader
	spans   *tracetest.SpanRecorder
	logs    *bytes.Buffer
}

func newRecoveryServer(t *testing.T, endpoint http.HandlerFunc) recoveryServer {
	t.Helper()
	logs := new(bytes.Buffer)
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	routes := chi.NewRouter()
	routes.Get("/panic", endpoint)
	reader := sdkmetric.NewManualReader()
	metrics, err := middleware.Metrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), routes)
	if err != nil {
		t.Fatal(err)
	}
	spans := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	handler := metrics(middleware.Recover(logger)(routes))
	handler = middleware.Logging(logger, routes)(handler)
	handler = middleware.Tracing(provider, routes)(handler)
	handler = middleware.Recover(logger)(handler)
	return recoveryServer{handler: handler, metrics: reader, spans: spans, logs: logs}
}

func serveAndRecover(handler http.Handler, w http.ResponseWriter) (value any) {
	defer func() { value = recover() }()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panic", http.NoBody))
	return nil
}

func assertNoActiveRequests(t *testing.T, reader *sdkmetric.ManualReader) {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "http.server.active_requests" {
				continue
			}
			sum, ok := item.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 0 {
				t.Fatalf("active requests did not return to zero: %+v", item.Data)
			}
			return
		}
	}
	t.Fatal("active request metric is missing")
}

func TestRecoverPreservesAbortHandler(t *testing.T) {
	server := newRecoveryServer(t, func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})
	recorder := httptest.NewRecorder()
	if value := serveAndRecover(server.handler, recorder); value != http.ErrAbortHandler {
		t.Fatalf("panic = %v, want ErrAbortHandler", value)
	}
	if recorder.Body.Len() != 0 || recorder.Header().Get("Content-Type") != "" {
		t.Fatalf("abort must not write a problem response: %s", recorder.Body.String())
	}
	assertNoActiveRequests(t, server.metrics)
}

func TestRecoverDoesNotOverwriteCommittedResponse(t *testing.T) {
	tests := []struct {
		name  string
		write func(http.ResponseWriter)
		code  int
		body  string
	}{
		{
			name: "explicit status",
			write: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusAccepted)
				w.WriteHeader(http.StatusBadRequest)
			},
			code: http.StatusAccepted,
		},
		{
			name: "implicit status",
			write: func(w http.ResponseWriter) {
				_, _ = w.Write([]byte("partial response"))
			},
			code: http.StatusOK,
			body: "partial response",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newRecoveryServer(t, func(w http.ResponseWriter, _ *http.Request) {
				tt.write(w)
				panic("failed after response started")
			})
			recorder := httptest.NewRecorder()
			if value := serveAndRecover(server.handler, recorder); value != http.ErrAbortHandler {
				t.Fatalf("panic = %v, want ErrAbortHandler", value)
			}
			if recorder.Code != tt.code || recorder.Body.String() != tt.body {
				t.Fatalf("committed response was changed: status=%d body=%q", recorder.Code, recorder.Body.String())
			}
			spans := server.spans.Ended()
			if len(spans) != 1 || spans[0].Status().Code != codes.Error {
				t.Fatalf("committed-response panic must mark its span as failed: %v", spans)
			}
			lines := strings.Split(strings.TrimSpace(server.logs.String()), "\n")
			if len(lines) != 1 {
				t.Fatalf("log records = %d, want 1", len(lines))
			}
			var record map[string]any
			if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
				t.Fatal(err)
			}
			if record["status"] != float64(tt.code) || record["msg"] != "request handled" || record["stack"] == nil {
				t.Fatalf("unexpected panic request log: %v", record)
			}
			assertNoActiveRequests(t, server.metrics)
		})
	}
}

// Unlike httptest.ResponseRecorder, this writer keeps informational responses
// separate from the final status, as net/http does on the wire.
type informationalRecorder struct {
	http.ResponseWriter
	statuses []int
}

func (w *informationalRecorder) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
	if status >= 200 {
		w.ResponseWriter.WriteHeader(status)
	}
}

func TestRecoverCanWriteProblemAfterInformationalResponse(t *testing.T) {
	server := newRecoveryServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		panic("failed after early hints")
	})
	recorder := httptest.NewRecorder()
	w := &informationalRecorder{ResponseWriter: recorder}
	if value := serveAndRecover(server.handler, w); value != nil {
		t.Fatalf("unexpected panic: %v", value)
	}
	if len(w.statuses) != 2 || w.statuses[0] != http.StatusEarlyHints || w.statuses[1] != http.StatusInternalServerError {
		t.Fatalf("unexpected response statuses: %v", w.statuses)
	}
	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("missing final problem response: %d %s", recorder.Code, recorder.Body.String())
	}
	assertNoActiveRequests(t, server.metrics)
}
