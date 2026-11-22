package position

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/infra/postgres/transaction"
)

var positionColumns = []string{"id", "trip_id", "latitude", "longitude", "recorded_at"}

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func New(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	return &Repository{pool: pool, timeout: timeout}
}

func (r *Repository) Save(ctx context.Context, value trip.Position) (trip.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := sq.Insert("trip_positions").
		Columns("trip_id", "latitude", "longitude", "recorded_at").
		Values(value.TripID, value.Coordinates.Latitude, value.Coordinates.Longitude, value.RecordedAt).
		Suffix("RETURNING id, trip_id, latitude, longitude, recorded_at").
		PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return trip.Position{}, fmt.Errorf("build insert position: %w", err)
	}
	saved, err := scanPosition(transaction.ExecutorFrom(ctx, r.pool).QueryRow(ctx, query, args...))
	if err != nil {
		return trip.Position{}, fmt.Errorf("insert position: %w", err)
	}
	return saved, nil
}

func (r *Repository) List(ctx context.Context, tripID uuid.UUID) ([]trip.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := sq.Select(positionColumns...).From("trip_positions").
		Where(sq.Eq{"trip_id": tripID}).OrderBy("recorded_at", "id").
		PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select positions: %w", err)
	}
	rows, err := transaction.ExecutorFrom(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select positions: %w", err)
	}
	defer rows.Close()
	result := []trip.Position{}
	for rows.Next() {
		value, err := scanPosition(rows)
		if err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read positions: %w", err)
	}
	return result, nil
}

func scanPosition(row pgx.Row) (trip.Position, error) {
	var value trip.Position
	err := row.Scan(&value.ID, &value.TripID, &value.Coordinates.Latitude, &value.Coordinates.Longitude, &value.RecordedAt)
	if err != nil {
		return trip.Position{}, err
	}
	value.RecordedAt = value.RecordedAt.UTC()
	return value, nil
}
