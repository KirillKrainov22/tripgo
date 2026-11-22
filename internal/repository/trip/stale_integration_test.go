//go:build integration

package trip_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	triprepo "github.com/KirillKrainov22/tripgo/internal/repository/trip"
)

func createStartedAt(t *testing.T, pool *pgxpool.Pool, repo *triprepo.Repository, startedAt time.Time) trip.Trip {
	t.Helper()
	value := trip.New(uuid.New(), trip.Data{UserID: uuid.New(), DriverID: uuid.New(), Price: 100}, startedAt)
	deleteAfterTest(t, pool, value.ID)
	if _, err := repo.Create(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	return value
}

func lastPositionAt(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) *time.Time {
	t.Helper()
	var value *time.Time
	if err := pool.QueryRow(context.Background(), "SELECT last_position_at FROM trips WHERE id = $1", id).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFindStaleSelectsOnlySilentActiveTrips(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	now := time.Now()

	withoutPositions := createStartedAt(t, pool, repo, now.Add(-2*time.Minute))
	oldPosition := createStartedAt(t, pool, repo, now.Add(-10*time.Minute))
	if err := repo.TouchLastPosition(context.Background(), oldPosition.ID, now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	fresh := createStartedAt(t, pool, repo, now.Add(-10*time.Minute))
	if err := repo.TouchLastPosition(context.Background(), fresh.ID, now); err != nil {
		t.Fatal(err)
	}
	justStarted := createStartedAt(t, pool, repo, now)
	completed := createStartedAt(t, pool, repo, now.Add(-10*time.Minute))
	if err := completed.Finish(now); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveFinished(context.Background(), completed); err != nil {
		t.Fatal(err)
	}

	stale, err := repo.FindStale(context.Background(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	position := map[uuid.UUID]int{}
	for i, item := range stale {
		position[item.TripID] = i
	}
	for _, excluded := range []uuid.UUID{fresh.ID, justStarted.ID, completed.ID} {
		if _, ok := position[excluded]; ok {
			t.Fatalf("trip %s must not be stale", excluded)
		}
	}
	first, okOld := position[oldPosition.ID]
	second, okNew := position[withoutPositions.ID]
	if !okOld || !okNew {
		t.Fatalf("stale trips are missing: %+v", stale)
	}
	if first > second {
		t.Fatal("trip silent for longer must come first")
	}
	if stale[second].DriverID != withoutPositions.DriverID {
		t.Fatalf("driver_id = %s, want %s", stale[second].DriverID, withoutPositions.DriverID)
	}
}

func TestTouchLastPositionIgnoresLatePositions(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	value := createStartedAt(t, pool, repo, time.Now().Add(-time.Hour))
	newer := trip.Timestamp(time.Now().Add(-time.Minute))
	older := newer.Add(-10 * time.Minute)

	if err := repo.TouchLastPosition(context.Background(), value.ID, newer); err != nil {
		t.Fatal(err)
	}
	if err := repo.TouchLastPosition(context.Background(), value.ID, older); err != nil {
		t.Fatal(err)
	}

	got := lastPositionAt(t, pool, value.ID)
	if got == nil || !got.Equal(newer) {
		t.Fatalf("last_position_at = %v, want %v", got, newer)
	}
}

func TestTouchLastPositionChecksTripStatus(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	completed := createStartedAt(t, pool, repo, time.Now().Add(-time.Hour))
	if err := completed.Finish(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveFinished(context.Background(), completed); err != nil {
		t.Fatal(err)
	}

	if err := repo.TouchLastPosition(context.Background(), completed.ID, time.Now()); !errors.Is(err, trip.ErrTripCompleted) {
		t.Fatalf("completed trip error = %v, want ErrTripCompleted", err)
	}
	if err := repo.TouchLastPosition(context.Background(), uuid.New(), time.Now()); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("unknown trip error = %v, want ErrTripNotFound", err)
	}
}
