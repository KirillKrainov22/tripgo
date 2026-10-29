package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type recorder struct {
	http.ResponseWriter
	status int
}

func newRecorder(w http.ResponseWriter) *recorder {
	return &recorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func routePattern(routes chi.Routes, r *http.Request) string {
	pattern := routes.Find(chi.NewRouteContext(), r.Method, r.URL.Path)
	if pattern == "" {
		return "unknown"
	}
	return pattern
}
