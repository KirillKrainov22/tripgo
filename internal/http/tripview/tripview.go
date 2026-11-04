package tripview

import (
	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	api "github.com/KirillKrainov22/tripgo/internal/generated"
)

func FromDomain(value trip.Trip) api.Trip {
	return api.Trip{
		Id:         value.ID,
		UserId:     value.UserID,
		DriverId:   value.DriverID,
		StartPoint: api.Coordinates{Latitude: value.StartPoint.Latitude, Longitude: value.StartPoint.Longitude},
		EndPoint:   api.Coordinates{Latitude: value.EndPoint.Latitude, Longitude: value.EndPoint.Longitude},
		Price:      value.Price,
		Status:     api.TripStatus(value.Status),
		StartedAt:  value.StartedAt,
		FinishedAt: value.FinishedAt,
	}
}
