package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/trip"
)

type handler struct {
	trips        *trip.Service
	ping         func(context.Context) error
	readyTimeout time.Duration
	logger       *slog.Logger
}

func NewHandler(trips *trip.Service, ping func(context.Context) error, readyTimeout time.Duration, logger *slog.Logger) http.Handler {
	h := &handler{trips: trips, ping: ping, readyTimeout: readyTimeout, logger: logger}
	router := chi.NewRouter()
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		h.problem(w, r, http.StatusNotFound, "route_not_found", "Route not found", "The requested route does not exist")
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		h.problem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "The HTTP method is not supported for this route")
	})
	return api.HandlerWithOptions(h, api.ChiServerOptions{
		BaseRouter: router,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			h.problem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "Invalid request parameter")
		},
	})
}

func (h *handler) Health(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (h *handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.readyTimeout)
	defer cancel()

	if err := h.ping(ctx); err != nil {
		h.logger.Warn("readiness check failed", "error", err)
		h.writeJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}
	h.writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}
