package idempotency

import (
	"context"
	"log/slog"
	"time"
)

type Cleaner struct {
	keys     keyRemover
	interval time.Duration
	logger   *slog.Logger
}

func NewCleaner(keys keyRemover, interval time.Duration, logger *slog.Logger) *Cleaner {
	return &Cleaner{keys: keys, interval: interval, logger: logger}
}

func (c *Cleaner) Run(ctx context.Context) {
	c.logger.InfoContext(ctx, "idempotency cleaner started")
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		c.clean(ctx)
		select {
		case <-ctx.Done():
			c.logger.InfoContext(ctx, "idempotency cleaner stopped")
			return
		case <-ticker.C:
		}
	}
}

func (c *Cleaner) clean(ctx context.Context) {
	deleted, err := c.keys.DeleteExpired(ctx)
	switch {
	case ctx.Err() != nil:
		return
	case err != nil:
		c.logger.ErrorContext(ctx, "idempotency keys cleanup failed", "error", err)
	case deleted > 0:
		c.logger.InfoContext(ctx, "expired idempotency keys deleted", "count", deleted)
	}
}
