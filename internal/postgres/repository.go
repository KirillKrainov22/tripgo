package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KirillKrainov22/tripgo/internal/trip"
)

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	return &Repository{pool: pool, timeout: timeout}
}

var tripColumns = []string{
	"id", "user_id", "driver_id", "start_latitude", "start_longitude",
	"end_latitude", "end_longitude", "price", "status", "started_at", "finished_at",
}

func (r *Repository) Create(ctx context.Context, value trip.Trip) (trip.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := sq.Insert("trips").Columns(tripColumns...).
		Values(value.ID, value.UserID, value.DriverID, value.StartPoint.Latitude, value.StartPoint.Longitude,
			value.EndPoint.Latitude, value.EndPoint.Longitude, value.Price, value.Status, value.StartedAt, value.FinishedAt).
		Suffix("RETURNING " + strings.Join(tripColumns, ", ")).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build insert trip: %w", err)
	}
	created, err := scanTrip(executorFor(ctx, r.pool).QueryRow(ctx, query, args...))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "trips_driver_active_uidx" {
			return trip.Trip{}, trip.ErrDriverBusy
		}
		return trip.Trip{}, fmt.Errorf("insert trip: %w", err)
	}
	return created, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := sq.Select(tripColumns...).From("trips").Where(sq.Eq{"id": id}).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build select trip: %w", err)
	}
	value, err := scanTrip(executorFor(ctx, r.pool).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return trip.Trip{}, trip.ErrNotFound
	}
	if err != nil {
		return trip.Trip{}, fmt.Errorf("select trip: %w", err)
	}
	return value, nil
}

func (r *Repository) Finish(ctx context.Context, id uuid.UUID, at time.Time) (trip.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := sq.Update("trips").Set("status", trip.Completed).Set("finished_at", at).Set("updated_at", at).
		Where(sq.Eq{"id": id, "status": trip.Active}).Suffix("RETURNING " + strings.Join(tripColumns, ", ")).
		PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build finish trip: %w", err)
	}
	value, err := scanTrip(executorFor(ctx, r.pool).QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := r.Get(ctx, id); err != nil {
			return trip.Trip{}, err
		}
		return trip.Trip{}, trip.ErrCompleted
	}
	if err != nil {
		return trip.Trip{}, fmt.Errorf("finish trip: %w", err)
	}
	return value, nil
}

func (r *Repository) RecordStatus(ctx context.Context, id uuid.UUID, from *string, to string, at time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := sq.Insert("trip_status_history").Columns("trip_id", "from_status", "to_status", "changed_at").
		Values(id, from, to, at).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip history: %w", err)
	}
	if _, err := executorFor(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert trip history: %w", err)
	}
	return nil
}

func scanTrip(row pgx.Row) (trip.Trip, error) {
	var value trip.Trip
	err := row.Scan(&value.ID, &value.UserID, &value.DriverID, &value.StartPoint.Latitude, &value.StartPoint.Longitude,
		&value.EndPoint.Latitude, &value.EndPoint.Longitude, &value.Price, &value.Status, &value.StartedAt, &value.FinishedAt)
	if err != nil {
		return trip.Trip{}, err
	}
	value.StartedAt = value.StartedAt.UTC()
	if value.FinishedAt != nil {
		finishedAt := value.FinishedAt.UTC()
		value.FinishedAt = &finishedAt
	}
	return value, nil
}
