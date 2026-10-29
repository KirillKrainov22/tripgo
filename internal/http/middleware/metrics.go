package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func Metrics(meter metric.Meter, routes chi.Routes) (func(http.Handler) http.Handler, error) {
	duration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10),
	)
	if err != nil {
		return nil, err
	}
	active, err := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Number of active HTTP server requests"),
	)
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			method := attribute.String("http.request.method", r.Method)
			route := attribute.String("http.route", routePattern(routes, r))
			active.Add(r.Context(), 1, metric.WithAttributes(method, route))
			rec := newRecorder(w)
			next.ServeHTTP(rec, r)
			active.Add(r.Context(), -1, metric.WithAttributes(method, route))
			status := attribute.Int("http.response.status_code", rec.status)
			duration.Record(r.Context(), time.Since(started).Seconds(), metric.WithAttributes(method, route, status))
		})
	}, nil
}
