//go:build integration

package trip_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/infra/postgres/transaction"
	triprepo "github.com/KirillKrainov22/tripgo/internal/repository/trip"
	"github.com/KirillKrainov22/tripgo/internal/usecase/trip/finish"
)

type noMetrics struct{}

func (noMetrics) TripCompleted(context.Context) {}

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

func newTrip(driverID uuid.UUID) trip.Trip {
	return trip.New(uuid.New(), trip.Data{
		UserID:     uuid.New(),
		DriverID:   driverID,
		StartPoint: trip.Coordinates{Latitude: 59.9398, Longitude: 30.3146},
		EndPoint:   trip.Coordinates{Latitude: 59.929, Longitude: 30.3626},
		Price:      1450,
	}, time.Now())
}

func deleteAfterTest(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM trips WHERE id = $1", id); err != nil {
			t.Errorf("cleanup trip %s: %v", id, err)
		}
	})
}

func historyCount(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(), "SELECT count(*) FROM trip_status_history WHERE trip_id = $1", id).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func TestCreateAndGet(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	value := newTrip(uuid.New())
	deleteAfterTest(t, pool, value.ID)

	created, err := repo.Create(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created.Data != value.Data || stored.Data != value.Data {
		t.Fatalf("stored data differs: %+v", stored)
	}
	if stored.Status != trip.StatusActive || !stored.StartedAt.Equal(value.StartedAt) || stored.FinishedAt != nil {
		t.Fatalf("unexpected stored trip: %+v", stored)
	}
}

func TestGetUnknownTrip(t *testing.T) {
	repo := triprepo.New(newPool(t), 3*time.Second)

	_, err := repo.Get(context.Background(), uuid.New())

	if !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("error = %v, want ErrTripNotFound", err)
	}
}

func TestSecondActiveTripForDriverIsRejected(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	driverID := uuid.New()
	first := newTrip(driverID)
	second := newTrip(driverID)
	deleteAfterTest(t, pool, first.ID)
	deleteAfterTest(t, pool, second.ID)

	if _, err := repo.Create(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	_, err := repo.Create(context.Background(), second)

	if !errors.Is(err, trip.ErrDriverBusy) {
		t.Fatalf("error = %v, want ErrDriverBusy", err)
	}
}

func TestCreateRollsBackWhenHistoryInsertFails(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	tx := transaction.NewManager(pool, 5*time.Second)
	value := newTrip(uuid.New())
	deleteAfterTest(t, pool, value.ID)

	err := tx.Do(context.Background(), func(ctx context.Context) error {
		if _, err := repo.Create(ctx, value); err != nil {
			return err
		}
		return repo.RecordStatus(ctx, trip.StatusChange{TripID: value.ID, To: trip.Status("unknown"), At: value.StartedAt})
	})

	if err == nil {
		t.Fatal("transaction must fail because of the history constraint")
	}
	if _, err := repo.Get(context.Background(), value.ID); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("trip must be rolled back, got error %v", err)
	}
	if count := historyCount(t, pool, value.ID); count != 0 {
		t.Fatalf("history rows = %d, want 0", count)
	}
}

func TestCreateCommitsBothTables(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	tx := transaction.NewManager(pool, 5*time.Second)
	value := newTrip(uuid.New())
	deleteAfterTest(t, pool, value.ID)

	err := tx.Do(context.Background(), func(ctx context.Context) error {
		if _, err := repo.Create(ctx, value); err != nil {
			return err
		}
		return repo.RecordStatus(ctx, trip.StatusChange{TripID: value.ID, To: trip.StatusActive, At: value.StartedAt})
	})
	if err != nil {
		t.Fatal(err)
	}
	if count := historyCount(t, pool, value.ID); count != 1 {
		t.Fatalf("history rows = %d, want 1", count)
	}
}

func TestSaveFinished(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	value := newTrip(uuid.New())
	deleteAfterTest(t, pool, value.ID)
	if _, err := repo.Create(context.Background(), value); err != nil {
		t.Fatal(err)
	}

	if err := value.Finish(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveFinished(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != trip.StatusCompleted || stored.FinishedAt == nil || !stored.FinishedAt.Equal(*value.FinishedAt) {
		t.Fatalf("unexpected stored trip: %+v", stored)
	}
	if err := repo.SaveFinished(context.Background(), value); !errors.Is(err, trip.ErrTripCompleted) {
		t.Fatalf("second finish error = %v, want ErrTripCompleted", err)
	}
}

func TestConcurrentFinishCompletesTripOnce(t *testing.T) {
	pool := newPool(t)
	repo := triprepo.New(pool, 3*time.Second)
	value := newTrip(uuid.New())
	deleteAfterTest(t, pool, value.ID)
	if _, err := repo.Create(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	usecase := finish.New(repo, transaction.NewManager(pool, 5*time.Second), noMetrics{}, slog.New(slog.DiscardHandler))

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := usecase.Finish(context.Background(), value.ID)
			results <- err
		})
	}
	wg.Wait()
	close(results)

	succeeded, rejected := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, trip.ErrTripCompleted):
			rejected++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("succeeded = %d, rejected = %d, want 1 and 1", succeeded, rejected)
	}
	if count := historyCount(t, pool, value.ID); count != 1 {
		t.Fatalf("history rows = %d, want 1", count)
	}
}
