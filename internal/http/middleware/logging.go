package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func Logging(logger *slog.Logger, routes chi.Routes) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			rec := newRecorder(w)
			next.ServeHTTP(rec, r)
			level := slog.LevelInfo
			if rec.status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			logger.LogAttrs(r.Context(), level, "request handled",
				slog.String("method", r.Method),
				slog.String("route", routePattern(routes, r)),
				slog.Int("status", rec.status),
				slog.Int64("duration_ms", time.Since(started).Milliseconds()),
			)
		})
	}
}
