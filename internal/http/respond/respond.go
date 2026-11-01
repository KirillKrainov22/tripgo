package respond

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/http/domainerror"
)

type failureKey struct{}

func WithFailure(ctx context.Context) (context.Context, *error) {
	var failure error
	return context.WithValue(ctx, failureKey{}, &failure), &failure
}

func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func Error(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	status, body := domainerror.Problem(err)
	if status >= http.StatusInternalServerError {
		if !RecordError(r, err) {
			logger.ErrorContext(r.Context(), "request failed", "error", err)
		}
	}
	Problem(w, r, status, body)
}

// RecordError annotates the span without writing a response. It reports whether
// the request logger will include the error, so callers can avoid duplicate logs.
func RecordError(r *http.Request, err error) bool {
	failure, hasRequestLog := r.Context().Value(failureKey{}).(*error)
	if hasRequestLog {
		*failure = err
	}
	span := trace.SpanFromContext(r.Context())
	span.RecordError(err)
	span.SetStatus(codes.Error, "internal error")
	span.SetAttributes(attribute.String("error.type", "internal_error"))
	return hasRequestLog
}

func Problem(w http.ResponseWriter, r *http.Request, status int, body api.Problem) {
	instance := r.URL.Path
	body.Instance = &instance
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
