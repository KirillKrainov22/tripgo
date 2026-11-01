package finish_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/usecase/trip/finish"
)

type fixture struct {
	trips   *MocktripRepository
	tx      *MocktxManager
	metrics *MocktripMetrics
	txCtx   context.Context
	logs    *bytes.Buffer
	usecase *finish.UseCase
}

type transactionContextKey struct{}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := fixture{
		trips:   NewMocktripRepository(ctrl),
		tx:      NewMocktxManager(ctrl),
		metrics: NewMocktripMetrics(ctrl),
		txCtx:   context.WithValue(context.Background(), transactionContextKey{}, true),
		logs:    &bytes.Buffer{},
	}
	f.usecase = finish.New(f.trips, f.tx, f.metrics, slog.New(slog.NewJSONHandler(f.logs, nil)))
	return f
}

func (f fixture) expectTransaction(commitErr error) {
	f.tx.EXPECT().Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, fn func(context.Context) error) error {
			if err := fn(f.txCtx); err != nil {
				return err
			}
			return commitErr
		})
}

func activeTrip() trip.Trip {
	return trip.New(uuid.New(), trip.Data{UserID: uuid.New(), DriverID: uuid.New(), Price: 100}, time.Now().Add(-time.Hour))
}

func TestFinishSuccess(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	current := activeTrip()
	f.trips.EXPECT().GetForUpdate(f.txCtx, current.ID).Return(current, nil)
	f.trips.EXPECT().SaveFinished(f.txCtx, gomock.Any()).
		DoAndReturn(func(_ context.Context, value trip.Trip) error {
			if value.Status != trip.StatusCompleted || value.FinishedAt == nil {
				t.Fatalf("trip is not finished: %+v", value)
			}
			return nil
		})
	f.trips.EXPECT().RecordStatus(f.txCtx, gomock.Any()).
		DoAndReturn(func(_ context.Context, change trip.StatusChange) error {
			if change.From == nil || *change.From != trip.StatusActive || change.To != trip.StatusCompleted {
				t.Fatalf("unexpected status change: %+v", change)
			}
			return nil
		})
	f.metrics.EXPECT().TripCompleted(gomock.Any())

	value, err := f.usecase.Finish(context.Background(), current.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value.Status != trip.StatusCompleted || value.FinishedAt == nil {
		t.Fatalf("unexpected trip: %+v", value)
	}
}

func TestFinishCompletedTrip(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	current := activeTrip()
	if err := current.Finish(time.Now()); err != nil {
		t.Fatal(err)
	}
	f.trips.EXPECT().GetForUpdate(f.txCtx, current.ID).Return(current, nil)

	_, err := f.usecase.Finish(context.Background(), current.ID)

	if !errors.Is(err, trip.ErrTripCompleted) {
		t.Fatalf("error = %v, want ErrTripCompleted", err)
	}
}

func TestFinishNotFound(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	id := uuid.New()
	f.trips.EXPECT().GetForUpdate(f.txCtx, id).Return(trip.Trip{}, trip.ErrTripNotFound)

	_, err := f.usecase.Finish(context.Background(), id)

	if !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("error = %v, want ErrTripNotFound", err)
	}
}

func TestFinishRepositoryError(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	current := activeTrip()
	dbErr := errors.New("deadline exceeded")
	f.trips.EXPECT().GetForUpdate(f.txCtx, current.ID).Return(current, nil)
	f.trips.EXPECT().SaveFinished(f.txCtx, gomock.Any()).Return(dbErr)

	_, err := f.usecase.Finish(context.Background(), current.ID)

	if !errors.Is(err, dbErr) {
		t.Fatalf("error = %v, want %v", err, dbErr)
	}
}

func TestFinishCommitErrorDoesNotReportSuccess(t *testing.T) {
	f := newFixture(t)
	commitErr := errors.New("commit failed")
	f.expectTransaction(commitErr)
	current := activeTrip()
	f.trips.EXPECT().GetForUpdate(f.txCtx, current.ID).Return(current, nil)
	f.trips.EXPECT().SaveFinished(f.txCtx, gomock.Any()).Return(nil)
	f.trips.EXPECT().RecordStatus(f.txCtx, gomock.Any()).Return(nil)

	_, err := f.usecase.Finish(context.Background(), current.ID)

	if !errors.Is(err, commitErr) {
		t.Fatalf("error = %v, want %v", err, commitErr)
	}
	if f.logs.Len() != 0 {
		t.Fatalf("failed commit must not log a successful completion: %s", f.logs.String())
	}
}
