package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/KirillKrainov22/tripgo/internal/http/domainerror"
	"github.com/KirillKrainov22/tripgo/internal/http/respond"
)

type panicError struct {
	value any
	stack []byte
}

func (e *panicError) Error() string {
	return "panic: " + fmt.Sprint(e.value)
}

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := newRecorder(w)
			defer func() {
				value := recover()
				if value == nil {
					return
				}
				if value == http.ErrAbortHandler {
					panic(value)
				}
				failure := &panicError{value: value, stack: debug.Stack()}
				if !respond.RecordError(r, failure) {
					logger.ErrorContext(r.Context(), "panic recovered",
						"panic", fmt.Sprint(value), "stack", string(failure.stack))
				}
				if rec.committed {
					// The status is already on the wire; let net/http abort the
					// incomplete response instead of appending a second body.
					panic(http.ErrAbortHandler)
				}
				status, body := domainerror.New(http.StatusInternalServerError, "internal_error", "Internal error", "An internal error occurred")
				respond.Problem(rec, r, status, body)
			}()
			next.ServeHTTP(rec, r)
		})
	}
}
