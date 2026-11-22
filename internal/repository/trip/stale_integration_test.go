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
	"github.com/KirillKrainov22/tripgo/internal/model"
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

func forEachStaleTrip(t *testing.T, repo *triprepo.Repository, staleAfter time.Duration, batchSize int, visit func(model.StaleTrip)) {
	t.Helper()
	var after *model.StaleTrip
	var previous *model.StaleTrip
	for {
		batch, err := repo.FindStale(t.Context(), staleAfter, after, batchSize)
		if err != nil {
			t.Fatal(err)
		}
		for i := range batch {
			item := batch[i]
			if previous != nil && (item.LastSeenAt.Before(previous.LastSeenAt) ||
				item.LastSeenAt.Equal(previous.LastSeenAt) && item.TripID.String() <= previous.TripID.String()) {
				t.Fatalf("pages are not ordered by last seen and id: %v after %v", item, *previous)
			}
			previous = &item
			visit(item)
		}
		if len(batch) < batchSize {
			return
		}
		after = &batch[len(batch)-1]
	}
}

func TestFindStaleSelectsOnlySilentActiveTrips(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	now := time.Now()

	withoutPositions := createStartedAt(t, pool, repo, now.Add(-2*time.Hour))
	oldPosition := createStartedAt(t, pool, repo, now.Add(-10*time.Hour))
	if err := repo.TouchLastPosition(context.Background(), oldPosition.ID, now.Add(-5*time.Hour)); err != nil {
		t.Fatal(err)
	}
	fresh := createStartedAt(t, pool, repo, now.Add(-10*time.Hour))
	if err := repo.TouchLastPosition(context.Background(), fresh.ID, now); err != nil {
		t.Fatal(err)
	}
	justStarted := createStartedAt(t, pool, repo, now)
	completed := createStartedAt(t, pool, repo, now.Add(-10*time.Hour))
	if err := completed.Finish(now); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveFinished(context.Background(), completed); err != nil {
		t.Fatal(err)
	}

	ours := map[uuid.UUID]trip.Trip{
		withoutPositions.ID: withoutPositions,
		oldPosition.ID:      oldPosition,
		fresh.ID:            fresh,
		justStarted.ID:      justStarted,
		completed.ID:        completed,
	}
	position := map[uuid.UUID]int{}
	ordinal := 0
	forEachStaleTrip(t, repo, time.Hour, 100, func(item model.StaleTrip) {
		if expected, ok := ours[item.TripID]; ok {
			position[item.TripID] = ordinal
			if item.DriverID != expected.DriverID {
				t.Fatalf("trip %s driver_id = %s, want %s", item.TripID, item.DriverID, expected.DriverID)
			}
		}
		ordinal++
	})
	for _, excluded := range []uuid.UUID{fresh.ID, justStarted.ID, completed.ID} {
		if _, ok := position[excluded]; ok {
			t.Fatalf("trip %s must not be stale", excluded)
		}
	}
	first, okOld := position[oldPosition.ID]
	second, okNew := position[withoutPositions.ID]
	if !okOld || !okNew {
		t.Fatalf("stale trips are missing: %+v", position)
	}
	if first > second {
		t.Fatal("trip silent for longer must come first")
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

func TestFindStalePagesByLastSeenAndID(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	startedAt := time.Now().Add(-time.Hour)
	ours := map[uuid.UUID]bool{}
	for range 5 {
		ours[createStartedAt(t, pool, repo, startedAt).ID] = true
	}

	seen := map[uuid.UUID]int{}
	forEachStaleTrip(t, repo, time.Minute, 2, func(item model.StaleTrip) {
		if ours[item.TripID] {
			seen[item.TripID]++
		}
	})
	if len(seen) != len(ours) {
		t.Fatalf("found %d of %d trips with the same last seen time", len(seen), len(ours))
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("trip %s was read %d times", id, count)
		}
	}
}
