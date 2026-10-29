package trip

import (
	"github.com/jackc/pgx/v5"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
)

const statusColumn = "status"

var tripColumns = []string{
	"id", "user_id", "driver_id", "start_latitude", "start_longitude",
	"end_latitude", "end_longitude", "price", statusColumn, "started_at", "finished_at",
}

func scanTrip(row pgx.Row) (trip.Trip, error) {
	var value trip.Trip
	var status string
	err := row.Scan(&value.ID, &value.UserID, &value.DriverID, &value.StartPoint.Latitude, &value.StartPoint.Longitude,
		&value.EndPoint.Latitude, &value.EndPoint.Longitude, &value.Price, &status, &value.StartedAt, &value.FinishedAt)
	if err != nil {
		return trip.Trip{}, err
	}
	value.Status = trip.Status(status)
	value.StartedAt = value.StartedAt.UTC()
	if value.FinishedAt != nil {
		finishedAt := value.FinishedAt.UTC()
		value.FinishedAt = &finishedAt
	}
	return value, nil
}
