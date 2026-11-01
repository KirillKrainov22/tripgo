package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/KirillKrainov22/tripgo/internal/http/respond"
)

func Logging(logger *slog.Logger, routes chi.Routes) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			ctx, failure := respond.WithFailure(r.Context())
			rec := newRecorder(w)
			returned := false
			defer func() {
				if !returned && !rec.committed && *failure == nil {
					return
				}
				level := slog.LevelInfo
				if rec.status >= http.StatusInternalServerError || *failure != nil {
					level = slog.LevelError
				}
				attrs := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("route", routePattern(routes, r)),
					slog.Int("status", rec.status),
					slog.Int64("duration_ms", time.Since(started).Milliseconds()),
				}
				if *failure != nil {
					attrs = append(attrs, slog.String("error", (*failure).Error()))
				}
				var panicFailure *panicError
				if errors.As(*failure, &panicFailure) {
					attrs = append(attrs, slog.String("panic", fmt.Sprint(panicFailure.value)), slog.String("stack", string(panicFailure.stack)))
				}
				logger.LogAttrs(ctx, level, "request handled", attrs...)
			}()
			next.ServeHTTP(rec, r.WithContext(ctx))
			returned = true
		})
	}
}
