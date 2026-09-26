package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type transactionKey struct{}

type transactionContext struct {
	pool    *pgxpool.Pool
	tx      pgx.Tx
	failure error
}

type TxManager struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewTxManager(pool *pgxpool.Pool, timeout time.Duration) *TxManager {
	return &TxManager{pool: pool, timeout: timeout}
}

func (m *TxManager) Do(ctx context.Context, fn func(context.Context) error) (err error) {
	if current, ok := ctx.Value(transactionKey{}).(*transactionContext); ok && current.pool == m.pool {
		defer func() {
			if value := recover(); value != nil {
				current.failure = errors.New("nested transaction callback panicked")
				panic(value)
			}
			if err != nil && current.failure == nil {
				current.failure = err
			}
		}()
		return fn(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), m.timeout)
		defer cleanupCancel()
		if rollbackErr := tx.Rollback(cleanupCtx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback transaction: %w", rollbackErr))
		}
	}()
	state := &transactionContext{pool: m.pool, tx: tx}
	txCtx := context.WithValue(ctx, transactionKey{}, state)
	if err := fn(txCtx); err != nil {
		return fmt.Errorf("execute transaction: %w", err)
	}
	if state.failure != nil {
		return fmt.Errorf("nested transaction failed: %w", state.failure)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

type executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func executorFor(ctx context.Context, pool *pgxpool.Pool) executor {
	if current, ok := ctx.Value(transactionKey{}).(*transactionContext); ok && current.pool == pool {
		return current.tx
	}
	return pool
}
