package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/KirillKrainov22/tripgo/internal/trip"
)

func (r *Repository) ReserveCreation(ctx context.Context, key uuid.UUID, candidate trip.Trip, ttl time.Duration) (stored trip.Trip, claimed bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	response, err := json.Marshal(candidate)
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("encode creation response: %w", err)
	}
	query, args, err := sq.Insert("trip_idempotency_keys").
		Columns("key", "response", "expires_at").
		Values(key, response, sq.Expr("clock_timestamp() + (? * interval '1 second')", ttl.Seconds())).
		Suffix(`ON CONFLICT (key) DO UPDATE
			SET response = EXCLUDED.response, expires_at = EXCLUDED.expires_at
			WHERE trip_idempotency_keys.expires_at <= clock_timestamp()
			RETURNING key`).
		PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("build reserve creation: %w", err)
	}
	var reserved uuid.UUID
	err = executorFor(ctx, r.pool).QueryRow(ctx, query, args...).Scan(&reserved)
	if err == nil {
		return candidate, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return trip.Trip{}, false, fmt.Errorf("reserve creation: %w", err)
	}

	query, args, err = sq.Select("response").From("trip_idempotency_keys").
		Where(sq.Eq{"key": key}).PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return trip.Trip{}, false, fmt.Errorf("build select creation response: %w", err)
	}
	if err := executorFor(ctx, r.pool).QueryRow(ctx, query, args...).Scan(&response); err != nil {
		return trip.Trip{}, false, fmt.Errorf("select creation response: %w", err)
	}
	if err := json.Unmarshal(response, &stored); err != nil {
		return trip.Trip{}, false, fmt.Errorf("decode creation response: %w", err)
	}
	return stored, false, nil
}
