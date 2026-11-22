package save

import (
	"log/slog"
	"net/http"

	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/http/positionview"
	"github.com/KirillKrainov22/tripgo/internal/http/respond"
)

type Handler struct {
	positions positionSaver
	logger    *slog.Logger
}

func New(positions positionSaver, logger *slog.Logger) *Handler {
	return &Handler{positions: positions, logger: logger}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	input, err := decodeInput(w, r, tripID)
	if err != nil {
		respond.Error(w, r, h.logger, err)
		return
	}
	saved, err := h.positions.Save(r.Context(), input)
	if err != nil {
		respond.Error(w, r, h.logger, err)
		return
	}
	respond.JSON(w, http.StatusCreated, positionview.FromDomain(saved))
}
