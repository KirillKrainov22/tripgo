//go:build integration

package position_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/infra/postgres/transaction"
	positionrepo "github.com/KirillKrainov22/tripgo/internal/repository/position"
	triprepo "github.com/KirillKrainov22/tripgo/internal/repository/trip"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL is not set: start the environment with tripgoctl and run make test-integration")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createTrip(t *testing.T, pool *pgxpool.Pool) trip.Trip {
	t.Helper()
	value := trip.New(uuid.New(), trip.Data{UserID: uuid.New(), DriverID: uuid.New(), Price: 100}, time.Now().Add(-time.Hour))
	if _, err := triprepo.New(pool, 3*time.Second).Create(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM trips WHERE id = $1", value.ID); err != nil {
			t.Errorf("cleanup trip %s: %v", value.ID, err)
		}
	})
	return value
}

func position(tripID uuid.UUID, recordedAt time.Time) trip.Position {
	return trip.Position{
		TripID:      tripID,
		Coordinates: trip.Coordinates{Latitude: 59.935, Longitude: 30.335},
		RecordedAt:  trip.Timestamp(recordedAt),
	}
}

func TestListSortsByRecordedAtThenID(t *testing.T) {
	pool := newPool(t)
	repo := positionrepo.New(pool, 3*time.Second)
	value := createTrip(t, pool)
	base := time.Now().Add(-10 * time.Minute)

	late, err := repo.Save(context.Background(), position(value.ID, base.Add(2*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	earlyFirst, err := repo.Save(context.Background(), position(value.ID, base))
	if err != nil {
		t.Fatal(err)
	}
	earlySecond, err := repo.Save(context.Background(), position(value.ID, base))
	if err != nil {
		t.Fatal(err)
	}

	route, err := repo.List(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(route) != 3 || route[0].ID != earlyFirst.ID || route[1].ID != earlySecond.ID || route[2].ID != late.ID {
		t.Fatalf("unexpected order: %+v", route)
	}
}

func TestSavePositionUpdatesTripAtomically(t *testing.T) {
	pool := newPool(t)
	trips := triprepo.New(pool, 3*time.Second)
	positions := positionrepo.New(pool, 3*time.Second)
	tx := transaction.NewManager(pool, 5*time.Second)
	value := createTrip(t, pool)
	recordedAt := trip.Timestamp(time.Now().Add(-time.Minute))

	err := tx.Do(context.Background(), func(ctx context.Context) error {
		if err := trips.TouchLastPosition(ctx, value.ID, recordedAt); err != nil {
			return err
		}
		_, err := positions.Save(ctx, position(value.ID, recordedAt))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var lastPosition *time.Time
	if err := pool.QueryRow(context.Background(), "SELECT last_position_at FROM trips WHERE id = $1", value.ID).Scan(&lastPosition); err != nil {
		t.Fatal(err)
	}
	if lastPosition == nil || !lastPosition.Equal(recordedAt) {
		t.Fatalf("last_position_at = %v, want %v", lastPosition, recordedAt)
	}

	broken := position(value.ID, time.Now())
	broken.Coordinates.Latitude = 100
	err = tx.Do(context.Background(), func(ctx context.Context) error {
		if err := trips.TouchLastPosition(ctx, value.ID, broken.RecordedAt); err != nil {
			return err
		}
		_, err := positions.Save(ctx, broken)
		return err
	})
	if err == nil {
		t.Fatal("insert with invalid latitude must fail")
	}
	if err := pool.QueryRow(context.Background(), "SELECT last_position_at FROM trips WHERE id = $1", value.ID).Scan(&lastPosition); err != nil {
		t.Fatal(err)
	}
	if !lastPosition.Equal(recordedAt) {
		t.Fatalf("last_position_at = %v after rollback, want %v", lastPosition, recordedAt)
	}
	route, err := positions.List(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(route) != 1 {
		t.Fatalf("positions after rollback = %d, want 1", len(route))
	}
}
