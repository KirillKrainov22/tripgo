package create_test

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
	"github.com/KirillKrainov22/tripgo/internal/model"
	"github.com/KirillKrainov22/tripgo/internal/usecase/trip/create"
)

type fixture struct {
	trips   *MocktripRepository
	keys    *MockidempotencyStore
	tx      *MocktxManager
	metrics *MocktripMetrics
	txCtx   context.Context
	logs    *bytes.Buffer
	usecase *create.UseCase
}

type transactionContextKey struct{}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := fixture{
		trips:   NewMocktripRepository(ctrl),
		keys:    NewMockidempotencyStore(ctrl),
		tx:      NewMocktxManager(ctrl),
		metrics: NewMocktripMetrics(ctrl),
		txCtx:   context.WithValue(context.Background(), transactionContextKey{}, true),
		logs:    &bytes.Buffer{},
	}
	f.usecase = create.New(f.trips, f.keys, f.tx, f.metrics, slog.New(slog.NewJSONHandler(f.logs, nil)), time.Hour)
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

func validData() trip.Data {
	return trip.Data{
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 59.9398, Longitude: 30.3146},
		EndPoint:   trip.Coordinates{Latitude: 59.929, Longitude: 30.3626},
		Price:      1450,
	}
}

func returnSame(_ context.Context, value trip.Trip) (trip.Trip, error) {
	return value, nil
}

func TestCreateSuccess(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	data := validData()
	var created trip.Trip
	f.trips.EXPECT().Create(f.txCtx, gomock.Any()).
		DoAndReturn(func(ctx context.Context, value trip.Trip) (trip.Trip, error) {
			created = value
			return value, nil
		})
	f.trips.EXPECT().RecordStatus(f.txCtx, gomock.Any()).
		DoAndReturn(func(_ context.Context, change trip.StatusChange) error {
			if change.TripID != created.ID || change.From != nil || change.To != trip.StatusActive {
				t.Fatalf("unexpected status change: %+v", change)
			}
			return nil
		})
	f.metrics.EXPECT().TripCreated(gomock.Any())

	result, err := f.usecase.Create(context.Background(), model.CreateTripInput{Data: data})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Replayed {
		t.Fatal("new trip must not be marked as replayed")
	}
	if result.Trip.Data != data || result.Trip.Status != trip.StatusActive || result.Trip.ID == uuid.Nil {
		t.Fatalf("unexpected trip: %+v", result.Trip)
	}
}

func TestCreateInvalidData(t *testing.T) {
	f := newFixture(t)
	data := validData()
	data.Price = -10

	_, err := f.usecase.Create(context.Background(), model.CreateTripInput{Data: data})

	var validation *trip.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
}

func TestCreateDriverBusy(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	f.trips.EXPECT().Create(f.txCtx, gomock.Any()).Return(trip.Trip{}, trip.ErrDriverBusy)

	_, err := f.usecase.Create(context.Background(), model.CreateTripInput{Data: validData()})

	if !errors.Is(err, trip.ErrDriverBusy) {
		t.Fatalf("error = %v, want ErrDriverBusy", err)
	}
}

func TestCreateRepositoryError(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	dbErr := errors.New("connection reset by peer")
	f.trips.EXPECT().Create(f.txCtx, gomock.Any()).DoAndReturn(returnSame)
	f.trips.EXPECT().RecordStatus(f.txCtx, gomock.Any()).Return(dbErr)

	_, err := f.usecase.Create(context.Background(), model.CreateTripInput{Data: validData()})

	if !errors.Is(err, dbErr) {
		t.Fatalf("error = %v, want %v", err, dbErr)
	}
}

func TestCreateIdempotencyReplay(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	data := validData()
	key := uuid.New()
	stored := trip.New(uuid.New(), data, time.Now().Add(-time.Minute))
	f.keys.EXPECT().Reserve(f.txCtx, key, gomock.Any(), time.Hour).Return(stored, false, nil)

	result, err := f.usecase.Create(context.Background(), model.CreateTripInput{Data: data, IdempotencyKey: &key})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Replayed || result.Trip.ID != stored.ID {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCreateIdempotencyConflict(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	key := uuid.New()
	stored := trip.New(uuid.New(), validData(), time.Now())
	f.keys.EXPECT().Reserve(f.txCtx, key, gomock.Any(), time.Hour).Return(stored, false, nil)

	_, err := f.usecase.Create(context.Background(), model.CreateTripInput{Data: validData(), IdempotencyKey: &key})

	if !errors.Is(err, trip.ErrIdempotencyConflict) {
		t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestCreateWithNewIdempotencyKey(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	key := uuid.New()
	f.keys.EXPECT().Reserve(f.txCtx, key, gomock.Any(), time.Hour).
		DoAndReturn(func(_ context.Context, _ uuid.UUID, candidate trip.Trip, _ time.Duration) (trip.Trip, bool, error) {
			return candidate, true, nil
		})
	f.trips.EXPECT().Create(f.txCtx, gomock.Any()).DoAndReturn(returnSame)
	f.trips.EXPECT().RecordStatus(f.txCtx, gomock.Any()).Return(nil)
	f.metrics.EXPECT().TripCreated(gomock.Any())

	result, err := f.usecase.Create(context.Background(), model.CreateTripInput{Data: validData(), IdempotencyKey: &key})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Replayed {
		t.Fatal("first request with a key must create a trip")
	}
}

func TestCreateCommitErrorDoesNotReportSuccess(t *testing.T) {
	f := newFixture(t)
	commitErr := errors.New("commit failed")
	f.expectTransaction(commitErr)
	f.trips.EXPECT().Create(f.txCtx, gomock.Any()).DoAndReturn(returnSame)
	f.trips.EXPECT().RecordStatus(f.txCtx, gomock.Any()).Return(nil)

	_, err := f.usecase.Create(context.Background(), model.CreateTripInput{Data: validData()})

	if !errors.Is(err, commitErr) {
		t.Fatalf("error = %v, want %v", err, commitErr)
	}
	if f.logs.Len() != 0 {
		t.Fatalf("failed commit must not log a successful creation: %s", f.logs.String())
	}
}
