package push_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/KirillKrainov22/tripgo/internal/gateway/push"
)

func TestCloseClosesIdleConnection(t *testing.T) {
	idle := make(chan net.Conn, 2)
	closed := make(chan net.Conn, 2)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	server.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		switch state {
		case http.StateIdle:
			idle <- conn
		case http.StateClosed:
			closed <- conn
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	client := push.New(server.URL, time.Second, noop.NewTracerProvider())
	t.Cleanup(client.Close)

	waitConnection := func(events <-chan net.Conn, state string) net.Conn {
		t.Helper()
		select {
		case conn := <-events:
			return conn
		case <-time.After(time.Second):
			t.Fatalf("connection did not become %s", state)
			return nil
		}
	}
	request := func() net.Conn {
		t.Helper()
		if err := client.RequestPosition(t.Context(), uuid.New(), uuid.New()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return waitConnection(idle, "idle")
	}

	first := request()
	if second := request(); second != first {
		t.Fatal("the second request did not reuse the idle connection")
	}
	client.Close()
	if conn := waitConnection(closed, "closed after Client.Close"); conn != first {
		t.Fatal("Client.Close closed an unexpected connection")
	}
}

func TestRequestPositionSendsCommand(t *testing.T) {
	tripID, driverID := uuid.New(), uuid.New()
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/push" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	client := push.New(server.URL, time.Second, noop.NewTracerProvider())
	defer client.Close()

	if err := client.RequestPosition(context.Background(), tripID, driverID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := got["data"].(map[string]any)
	if got["kind"] != "REQUEST_POSITION" || got["recipient_id"] != driverID.String() || data["trip_id"] != tripID.String() {
		t.Fatalf("unexpected body: %v", got)
	}
	if requestID, _ := got["request_id"].(string); requestID == "" {
		t.Fatal("request_id is empty")
	}
}

func TestRequestPositionPropagatesTraceContext(t *testing.T) {
	var traceparent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	provider := sdktrace.NewTracerProvider()
	client := push.New(server.URL, time.Second, provider)
	defer client.Close()
	ctx, span := provider.Tracer("test").Start(context.Background(), "worker")
	defer span.End()

	if err := client.RequestPosition(ctx, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if traceparent == "" || !strings.Contains(traceparent, span.SpanContext().TraceID().String()) {
		t.Fatalf("traceparent = %q, want trace id %s", traceparent, span.SpanContext().TraceID())
	}
}

func TestRequestPositionErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "bad request is rejected", status: http.StatusBadRequest, want: push.ErrRejected},
		{name: "rate limit is rejected", status: http.StatusTooManyRequests, want: push.ErrRejected},
		{name: "server error is unavailable", status: http.StatusServiceUnavailable, want: push.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()
			client := push.New(server.URL, time.Second, noop.NewTracerProvider())
			defer client.Close()

			err := client.RequestPosition(context.Background(), uuid.New(), uuid.New())

			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRequestPositionTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	defer close(release)
	client := push.New(server.URL, 50*time.Millisecond, noop.NewTracerProvider())
	defer client.Close()

	err := client.RequestPosition(context.Background(), uuid.New(), uuid.New())

	if err == nil || errors.Is(err, push.ErrRejected) || errors.Is(err, push.ErrUnavailable) {
		t.Fatalf("expected transport timeout, got %v", err)
	}
}
