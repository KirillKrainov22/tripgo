package list_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/usecase/position/list"
)

func TestListReturnsPositions(t *testing.T) {
	ctrl := gomock.NewController(t)
	trips := NewMocktripRepository(ctrl)
	positions := NewMockpositionRepository(ctrl)
	tripID := uuid.New()
	stored := []trip.Position{{ID: 1, TripID: tripID, RecordedAt: time.Now()}}
	trips.EXPECT().Get(gomock.Any(), tripID).Return(trip.Trip{ID: tripID}, nil)
	positions.EXPECT().List(gomock.Any(), tripID).Return(stored, nil)

	result, err := list.New(trips, positions).List(context.Background(), tripID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].ID != 1 {
		t.Fatalf("unexpected positions: %+v", result)
	}
}

func TestListUnknownTrip(t *testing.T) {
	ctrl := gomock.NewController(t)
	trips := NewMocktripRepository(ctrl)
	tripID := uuid.New()
	trips.EXPECT().Get(gomock.Any(), tripID).Return(trip.Trip{}, trip.ErrTripNotFound)

	_, err := list.New(trips, NewMockpositionRepository(ctrl)).List(context.Background(), tripID)

	if !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("error = %v, want ErrTripNotFound", err)
	}
}
