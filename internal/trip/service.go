package trip

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TxManager interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}

type Repository interface {
	Create(ctx context.Context, trip Trip) (Trip, error)
	Get(ctx context.Context, id uuid.UUID) (Trip, error)
	Finish(ctx context.Context, id uuid.UUID, at time.Time) (Trip, error)
	RecordStatus(ctx context.Context, id uuid.UUID, from *string, to string, at time.Time) error
	ReserveCreation(ctx context.Context, key uuid.UUID, candidate Trip, ttl time.Duration) (stored Trip, claimed bool, err error)
}

type Service struct {
	repository     Repository
	tx             TxManager
	idempotencyTTL time.Duration
}

func NewService(repository Repository, tx TxManager, idempotencyTTL time.Duration) *Service {
	return &Service{repository: repository, tx: tx, idempotencyTTL: idempotencyTTL}
}

type CreateResult struct {
	Trip     Trip
	Replayed bool
}

func (s *Service) Create(ctx context.Context, data Data, key *uuid.UUID) (CreateResult, error) {
	if err := data.Validate(); err != nil {
		return CreateResult{}, err
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return CreateResult{}, fmt.Errorf("generate trip ID: %w", err)
	}
	created := Trip{
		Data:      data,
		ID:        id,
		Status:    Active,
		StartedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	var result CreateResult
	err = s.tx.Do(ctx, func(ctx context.Context) error {
		if key != nil {
			stored, claimed, err := s.repository.ReserveCreation(ctx, *key, created, s.idempotencyTTL)
			if err != nil {
				return err
			}
			if !claimed {
				if stored.Data != data {
					return ErrIdempotencyConflict
				}
				result = CreateResult{Trip: stored, Replayed: true}
				return nil
			}
		}
		var err error
		created, err = s.repository.Create(ctx, created)
		if err != nil {
			return err
		}
		if err := s.repository.RecordStatus(ctx, created.ID, nil, Active, created.StartedAt); err != nil {
			return err
		}
		result.Trip = created
		return nil
	})
	if err != nil {
		return CreateResult{}, fmt.Errorf("create trip: %w", err)
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Trip, error) {
	return s.repository.Get(ctx, id)
}

func (s *Service) Finish(ctx context.Context, id uuid.UUID) (Trip, error) {
	var value Trip
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		value, err = s.repository.Finish(ctx, id, time.Now().UTC().Truncate(time.Microsecond))
		if err != nil {
			return err
		}
		from := Active
		return s.repository.RecordStatus(ctx, id, &from, Completed, *value.FinishedAt)
	})
	if err != nil {
		return Trip{}, fmt.Errorf("finish trip: %w", err)
	}
	return value, nil
}
