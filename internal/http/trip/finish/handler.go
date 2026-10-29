package finish

import (
	"log/slog"
	"net/http"

	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/http/jsonbody"
	"github.com/KirillKrainov22/tripgo/internal/http/respond"
	"github.com/KirillKrainov22/tripgo/internal/http/tripview"
)

type Handler struct {
	trips  tripFinisher
	logger *slog.Logger
}

func New(trips tripFinisher, logger *slog.Logger) *Handler {
	return &Handler{trips: trips, logger: logger}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, id api.TripId) {
	if err := jsonbody.RequireEmpty(w, r); err != nil {
		respond.Error(w, r, h.logger, err)
		return
	}
	value, err := h.trips.Finish(r.Context(), id)
	if err != nil {
		respond.Error(w, r, h.logger, err)
		return
	}
	respond.JSON(w, http.StatusOK, tripview.FromDomain(value))
}
