//go:build integration

package idempotency_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/repository/idempotency"
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

func deleteKeyAfterTest(t *testing.T, pool *pgxpool.Pool, key uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM trip_idempotency_keys WHERE key = $1", key); err != nil {
			t.Errorf("cleanup key %s: %v", key, err)
		}
	})
}

func keyExists(t *testing.T, pool *pgxpool.Pool, key uuid.UUID) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM trip_idempotency_keys WHERE key = $1)", key).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

func candidate() trip.Trip {
	return trip.New(uuid.New(), trip.Data{UserID: uuid.New(), DriverID: uuid.New(), Price: 500}, time.Now())
}

func TestReserveReturnsStoredTripForSameKey(t *testing.T) {
	pool := newPool(t)
	repo := idempotency.New(pool, 3*time.Second)
	key := uuid.New()
	deleteKeyAfterTest(t, pool, key)
	first := candidate()

	stored, claimed, err := repo.Reserve(context.Background(), key, first, time.Hour)
	if err != nil || !claimed || stored.ID != first.ID {
		t.Fatalf("first reserve: stored=%v claimed=%v err=%v", stored.ID, claimed, err)
	}
	stored, claimed, err = repo.Reserve(context.Background(), key, candidate(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if claimed || stored.ID != first.ID || stored.Data != first.Data {
		t.Fatalf("second reserve: stored=%+v claimed=%v", stored, claimed)
	}
}

func TestReserveReplacesExpiredKey(t *testing.T) {
	pool := newPool(t)
	repo := idempotency.New(pool, 3*time.Second)
	key := uuid.New()
	deleteKeyAfterTest(t, pool, key)

	if _, _, err := repo.Reserve(context.Background(), key, candidate(), -time.Second); err != nil {
		t.Fatal(err)
	}
	next := candidate()
	stored, claimed, err := repo.Reserve(context.Background(), key, next, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed || stored.ID != next.ID {
		t.Fatalf("expired key must be replaced: stored=%v claimed=%v", stored.ID, claimed)
	}
}

func TestDeleteExpiredRemovesOnlyExpiredKeys(t *testing.T) {
	pool := newPool(t)
	repo := idempotency.New(pool, 3*time.Second)
	expired, fresh := uuid.New(), uuid.New()
	deleteKeyAfterTest(t, pool, expired)
	deleteKeyAfterTest(t, pool, fresh)
	if _, _, err := repo.Reserve(context.Background(), expired, candidate(), -time.Second); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.Reserve(context.Background(), fresh, candidate(), time.Hour); err != nil {
		t.Fatal(err)
	}

	deleted, err := repo.DeleteExpired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if deleted < 1 {
		t.Fatalf("deleted = %d, want at least 1", deleted)
	}
	if keyExists(t, pool, expired) {
		t.Fatal("expired key is still stored")
	}
	if !keyExists(t, pool, fresh) {
		t.Fatal("fresh key was deleted")
	}
}
