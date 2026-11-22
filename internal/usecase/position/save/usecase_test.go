package save_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/model"
	"github.com/KirillKrainov22/tripgo/internal/usecase/position/save"
)

type fixture struct {
	trips     *MocktripRepository
	positions *MockpositionRepository
	tx        *MocktxManager
	requests  *MockrequestCanceler
	metrics   *MockpositionMetrics
	txCtx     context.Context
	usecase   *save.UseCase
}

type transactionContextKey struct{}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := fixture{
		trips:     NewMocktripRepository(ctrl),
		positions: NewMockpositionRepository(ctrl),
		tx:        NewMocktxManager(ctrl),
		requests:  NewMockrequestCanceler(ctrl),
		metrics:   NewMockpositionMetrics(ctrl),
		txCtx:     context.WithValue(context.Background(), transactionContextKey{}, true),
	}
	f.usecase = save.New(f.trips, f.positions, f.tx, f.requests, f.metrics)
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

func validInput() model.SavePositionInput {
	return model.SavePositionInput{
		TripID:      uuid.New(),
		Coordinates: trip.Coordinates{Latitude: 59.935, Longitude: 30.335},
		RecordedAt:  time.Now().Add(-time.Second),
	}
}

func TestSaveSuccessCancelsPendingRequest(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	input := validInput()
	f.trips.EXPECT().TouchLastPosition(f.txCtx, input.TripID, gomock.Any()).Return(nil)
	f.positions.EXPECT().Save(f.txCtx, gomock.Any()).
		DoAndReturn(func(_ context.Context, value trip.Position) (trip.Position, error) {
			value.ID = 42
			return value, nil
		})
	f.requests.EXPECT().Cancel(input.TripID)
	f.metrics.EXPECT().PositionSaved(gomock.Any())

	saved, err := f.usecase.Save(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved.ID != 42 || saved.TripID != input.TripID {
		t.Fatalf("unexpected position: %+v", saved)
	}
}

func TestSaveRejectsCompletedTrip(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	input := validInput()
	f.trips.EXPECT().TouchLastPosition(f.txCtx, input.TripID, gomock.Any()).Return(trip.ErrTripCompleted)

	_, err := f.usecase.Save(context.Background(), input)

	if !errors.Is(err, trip.ErrTripCompleted) {
		t.Fatalf("error = %v, want ErrTripCompleted", err)
	}
}

func TestSaveUnknownTrip(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	input := validInput()
	f.trips.EXPECT().TouchLastPosition(f.txCtx, input.TripID, gomock.Any()).Return(trip.ErrTripNotFound)

	_, err := f.usecase.Save(context.Background(), input)

	if !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("error = %v, want ErrTripNotFound", err)
	}
}

func TestSaveRejectsFuturePosition(t *testing.T) {
	f := newFixture(t)
	input := validInput()
	input.RecordedAt = time.Now().Add(10 * time.Minute)

	_, err := f.usecase.Save(context.Background(), input)

	var validation *trip.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
}

func TestSaveRepositoryErrorKeepsRequest(t *testing.T) {
	f := newFixture(t)
	f.expectTransaction(nil)
	input := validInput()
	dbErr := errors.New("insert failed")
	f.trips.EXPECT().TouchLastPosition(f.txCtx, input.TripID, gomock.Any()).Return(nil)
	f.positions.EXPECT().Save(f.txCtx, gomock.Any()).Return(trip.Position{}, dbErr)

	_, err := f.usecase.Save(context.Background(), input)

	if !errors.Is(err, dbErr) {
		t.Fatalf("error = %v, want %v", err, dbErr)
	}
}

func TestSaveCommitErrorKeepsRequest(t *testing.T) {
	f := newFixture(t)
	commitErr := errors.New("commit failed")
	f.expectTransaction(commitErr)
	input := validInput()
	f.trips.EXPECT().TouchLastPosition(f.txCtx, input.TripID, gomock.Any()).Return(nil)
	f.positions.EXPECT().Save(f.txCtx, gomock.Any()).
		DoAndReturn(func(_ context.Context, value trip.Position) (trip.Position, error) {
			value.ID = 42
			return value, nil
		})

	_, err := f.usecase.Save(context.Background(), input)

	if !errors.Is(err, commitErr) {
		t.Fatalf("error = %v, want %v", err, commitErr)
	}
}
