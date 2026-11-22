package positionview

import (
	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	api "github.com/KirillKrainov22/tripgo/internal/generated"
)

func FromDomain(value trip.Position) api.TripPosition {
	return api.TripPosition{
		Id:          value.ID,
		TripId:      value.TripID,
		Coordinates: api.Coordinates{Latitude: value.Coordinates.Latitude, Longitude: value.Coordinates.Longitude},
		RecordedAt:  value.RecordedAt,
	}
}

func List(values []trip.Position) api.TripPositionsResponse {
	positions := make([]api.TripPosition, 0, len(values))
	for _, value := range values {
		positions = append(positions, FromDomain(value))
	}
	return api.TripPositionsResponse{Positions: positions}
}
