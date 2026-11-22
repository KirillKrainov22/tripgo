package trip

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/infra/postgres/transaction"
	"github.com/KirillKrainov22/tripgo/internal/model"
)

const lastSeenExpr = "COALESCE(last_position_at, started_at)"

func (r *Repository) TouchLastPosition(ctx context.Context, id uuid.UUID, recordedAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := sq.Update("trips").
		Set("last_position_at", sq.Expr("GREATEST(last_position_at, ?::timestamptz)", recordedAt)).
		Where(sq.Eq{"id": id, statusColumn: string(trip.StatusActive)}).
		PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return fmt.Errorf("build touch last position: %w", err)
	}
	tag, err := transaction.ExecutorFrom(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("touch last position: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	return trip.ErrTripCompleted
}

func (r *Repository) FindStale(ctx context.Context, staleAfter time.Duration) ([]model.StaleTrip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, err := sq.Select("id", "driver_id", lastSeenExpr).From("trips").
		Where(sq.Eq{statusColumn: string(trip.StatusActive)}).
		Where(lastSeenExpr+" < now() - make_interval(secs => ?)", staleAfter.Seconds()).
		OrderBy(lastSeenExpr).
		PlaceholderFormat(sq.Dollar).ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select stale trips: %w", err)
	}
	rows, err := transaction.ExecutorFrom(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select stale trips: %w", err)
	}
	defer rows.Close()
	var result []model.StaleTrip
	for rows.Next() {
		var item model.StaleTrip
		if err := rows.Scan(&item.TripID, &item.DriverID, &item.LastSeenAt); err != nil {
			return nil, fmt.Errorf("scan stale trip: %w", err)
		}
		item.LastSeenAt = item.LastSeenAt.UTC()
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stale trips: %w", err)
	}
	return result, nil
}
