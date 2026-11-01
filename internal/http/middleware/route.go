package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type recorder struct {
	http.ResponseWriter
	status    int
	committed bool
}

func newRecorder(w http.ResponseWriter) *recorder {
	return &recorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *recorder) WriteHeader(status int) {
	if r.committed {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		r.ResponseWriter.WriteHeader(status)
		return
	}
	r.status = status
	r.committed = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(body []byte) (int, error) {
	if !r.committed {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(body)
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
