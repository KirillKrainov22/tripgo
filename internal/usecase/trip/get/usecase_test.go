package get_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	"github.com/KirillKrainov22/tripgo/internal/usecase/trip/get"
)

func TestGetSuccess(t *testing.T) {
	trips := NewMocktripRepository(gomock.NewController(t))
	stored := trip.New(uuid.New(), trip.Data{UserID: uuid.New(), DriverID: uuid.New()}, time.Now())
	trips.EXPECT().Get(gomock.Any(), stored.ID).Return(stored, nil)

	value, err := get.New(trips).Get(context.Background(), stored.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value.ID != stored.ID {
		t.Fatalf("id = %s, want %s", value.ID, stored.ID)
	}
}

func TestGetNotFound(t *testing.T) {
	trips := NewMocktripRepository(gomock.NewController(t))
	id := uuid.New()
	trips.EXPECT().Get(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)

	_, err := get.New(trips).Get(context.Background(), id)

	if !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("error = %v, want ErrTripNotFound", err)
	}
}
