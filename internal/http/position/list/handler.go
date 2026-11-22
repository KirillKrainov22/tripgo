package list

import (
	"log/slog"
	"net/http"

	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/http/positionview"
	"github.com/KirillKrainov22/tripgo/internal/http/respond"
)

type Handler struct {
	positions positionLister
	logger    *slog.Logger
}

func New(positions positionLister, logger *slog.Logger) *Handler {
	return &Handler{positions: positions, logger: logger}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	positions, err := h.positions.List(r.Context(), tripID)
	if err != nil {
		respond.Error(w, r, h.logger, err)
		return
	}
	respond.JSON(w, http.StatusOK, positionview.List(positions))
}
