package trip

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/infra/postgres/transaction"
)

const driverActiveIndex = "trips_driver_active_uidx"

func (r *Repository) Create(ctx context.Context, value trip.Trip) (trip.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := sq.Insert("trips").Columns(tripColumns...).
		Values(value.ID, value.UserID, value.DriverID, value.StartPoint.Latitude, value.StartPoint.Longitude,
			value.EndPoint.Latitude, value.EndPoint.Longitude, value.Price, string(value.Status), value.StartedAt, value.FinishedAt).
		Suffix("RETURNING " + strings.Join(tripColumns, ", ")).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build insert trip: %w", err)
	}
	created, err := scanTrip(transaction.ExecutorFrom(ctx, r.pool).QueryRow(ctx, query, args...))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == driverActiveIndex {
			return trip.Trip{}, trip.ErrDriverBusy
		}
		return trip.Trip{}, fmt.Errorf("insert trip: %w", err)
	}
	return created, nil
}

func (r *Repository) RecordStatus(ctx context.Context, change trip.StatusChange) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var from *string
	if change.From != nil {
		value := string(*change.From)
		from = &value
	}
	query, args, err := sq.Insert("trip_status_history").Columns("trip_id", "from_status", "to_status", "changed_at").
		Values(change.TripID, from, string(change.To), change.At).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip history: %w", err)
	}
	if _, err := transaction.ExecutorFrom(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert trip history: %w", err)
	}
	return nil
}

func (r *Repository) SaveFinished(ctx context.Context, value trip.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := sq.Update("trips").
		Set(statusColumn, string(value.Status)).
		Set("finished_at", value.FinishedAt).
		Set("updated_at", value.FinishedAt).
		Where(sq.Eq{"id": value.ID, statusColumn: string(trip.StatusActive)}).
		PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return fmt.Errorf("build finish trip: %w", err)
	}
	tag, err := transaction.ExecutorFrom(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("finish trip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return trip.ErrTripCompleted
	}
	return nil
}
