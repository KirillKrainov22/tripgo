package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/KirillKrainov22/tripgo/internal/http/domainerror"
	"github.com/KirillKrainov22/tripgo/internal/http/respond"
)

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				value := recover()
				if value == nil {
					return
				}
				if value == http.ErrAbortHandler {
					panic(value)
				}
				logger.ErrorContext(r.Context(), "panic recovered",
					"panic", fmt.Sprint(value),
					"stack", string(debug.Stack()),
				)
				status, body := domainerror.New(http.StatusInternalServerError, "internal_error", "Internal error", "An internal error occurred")
				respond.Problem(w, r, status, body)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
