package create

import (
	"log/slog"
	"net/http"

	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/http/respond"
	"github.com/KirillKrainov22/tripgo/internal/http/tripview"
)

type Handler struct {
	trips  tripCreator
	logger *slog.Logger
}

func New(trips tripCreator, logger *slog.Logger) *Handler {
	return &Handler{trips: trips, logger: logger}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	input, err := decodeInput(w, r)
	if err != nil {
		respond.Error(w, r, h.logger, err)
		return
	}
	input.IdempotencyKey = params.IdempotencyKey
	result, err := h.trips.Create(r.Context(), input)
	if err != nil {
		respond.Error(w, r, h.logger, err)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	w.Header().Set("Location", "/api/v1/trips/"+result.Trip.ID.String())
	respond.JSON(w, status, tripview.FromDomain(result.Trip))
}
