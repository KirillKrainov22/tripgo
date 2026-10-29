package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
)

func Tracing(provider trace.TracerProvider, routes chi.Routes) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		withRoute := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("http.route", routePattern(routes, r)))
			next.ServeHTTP(w, r)
		})
		return otelhttp.NewHandler(withRoute, "http.server",
			otelhttp.WithTracerProvider(provider),
			otelhttp.WithMeterProvider(noop.NewMeterProvider()),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return r.Method + " " + routePattern(routes, r)
			}),
		)
	}
}
