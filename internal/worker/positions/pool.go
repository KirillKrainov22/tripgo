package positions

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/KirillKrainov22/tripgo/internal/model"
)

type Config struct {
	CheckInterval  time.Duration
	StaleAfter     time.Duration
	Workers        int
	QueueSize      int
	RequestTimeout time.Duration
}

type task struct {
	trip  model.StaleTrip
	token uint64
	link  trace.SpanContext
}

type Pool struct {
	cfg       Config
	trips     staleTripFinder
	push      pushGateway
	newTicker func(time.Duration) Ticker
	metrics   *metrics
	tracer    trace.Tracer
	logger    *slog.Logger

	queue     chan task
	mu        sync.Mutex
	pending   map[uuid.UUID]uint64
	nextToken uint64
	active    atomic.Int64

	stopScan   context.CancelFunc
	cancelWork context.CancelFunc
	scanDone   chan struct{}
	workers    sync.WaitGroup
}

func New(
	cfg Config,
	trips staleTripFinder,
	push pushGateway,
	newTicker func(time.Duration) Ticker,
	meter metric.Meter,
	tracerProvider trace.TracerProvider,
	logger *slog.Logger,
) (*Pool, error) {
	p := &Pool{
		cfg:       cfg,
		trips:     trips,
		push:      push,
		newTicker: newTicker,
		tracer:    tracerProvider.Tracer("github.com/KirillKrainov22/tripgo/internal/worker/positions"),
		logger:    logger,
		queue:     make(chan task, cfg.QueueSize),
		pending:   make(map[uuid.UUID]uint64),
		scanDone:  make(chan struct{}),
	}
	m, err := newMetrics(meter,
		func() int64 { return int64(len(p.queue)) },
		p.active.Load,
	)
	if err != nil {
		return nil, err
	}
	p.metrics = m
	return p, nil
}

func (p *Pool) Start(ctx context.Context) {
	scanCtx, stopScan := context.WithCancel(ctx)
	workCtx, cancelWork := context.WithCancel(ctx)
	p.stopScan = stopScan
	p.cancelWork = cancelWork
	for range p.cfg.Workers {
		p.workers.Go(func() {
			p.work(workCtx)
		})
	}
	go func() {
		defer close(p.scanDone)
		p.scanLoop(scanCtx)
	}()
	p.logger.InfoContext(ctx, "position poller started", "workers", p.cfg.Workers, "queue_size", p.cfg.QueueSize)
}

func (p *Pool) StopScanner() {
	p.stopScan()
	<-p.scanDone
}

func (p *Pool) StopWorkers(ctx context.Context) error {
	close(p.queue)
	done := make(chan struct{})
	go func() {
		p.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		p.cancelWork()
		p.logger.InfoContext(ctx, "position poller stopped")
		return nil
	case <-ctx.Done():
		p.cancelWork()
		<-done
		return fmt.Errorf("position workers did not finish queued tasks: %w", ctx.Err())
	}
}

func (p *Pool) Cancel(tripID uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pending, tripID)
}

func (p *Pool) scanLoop(ctx context.Context) {
	ticker := p.newTicker(p.cfg.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			p.scan(ctx)
		}
	}
}

func (p *Pool) scan(ctx context.Context) {
	ctx, span := p.tracer.Start(ctx, "positions.scan")
	defer span.End()
	trips, err := p.trips.FindStale(ctx, p.cfg.StaleAfter)
	if err != nil {
		if ctx.Err() == nil {
			p.logger.ErrorContext(ctx, "stale trips scan failed", "error", err)
			span.RecordError(err)
			span.SetStatus(codes.Error, "scan failed")
		}
		return
	}
	span.SetAttributes(attribute.Int("tripgo.stale_trips", len(trips)))
	for _, item := range trips {
		result := p.enqueue(item, span.SpanContext())
		p.metrics.staleChecks.Add(ctx, 1, resultAttr(result))
		if result == resultDropped {
			p.metrics.requests.Add(ctx, 1, resultAttr(resultDropped))
			p.logger.WarnContext(ctx, "position request dropped: queue is full", "trip_id", item.TripID.String())
		}
	}
}

func (p *Pool) enqueue(item model.StaleTrip, link trace.SpanContext) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.pending[item.TripID]; ok {
		return resultDuplicate
	}
	p.nextToken++
	next := task{trip: item, token: p.nextToken, link: link}
	select {
	case p.queue <- next:
		p.pending[item.TripID] = next.token
		return resultQueued
	default:
		return resultDropped
	}
}

func (p *Pool) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case current, ok := <-p.queue:
			if !ok {
				return
			}
			if p.isPending(current) {
				p.process(ctx, current)
				p.forget(current)
			}
		}
	}
}

func (p *Pool) isPending(current task) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	token, ok := p.pending[current.trip.TripID]
	return ok && token == current.token
}

func (p *Pool) forget(current task) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending[current.trip.TripID] == current.token {
		delete(p.pending, current.trip.TripID)
	}
}

func (p *Pool) process(ctx context.Context, current task) {
	p.active.Add(1)
	defer p.active.Add(-1)
	ctx, cancel := context.WithTimeout(ctx, p.cfg.RequestTimeout)
	defer cancel()
	ctx, span := p.tracer.Start(ctx, "positions.request",
		trace.WithLinks(trace.Link{SpanContext: current.link}),
		trace.WithAttributes(attribute.String("tripgo.trip_id", current.trip.TripID.String())),
	)
	defer span.End()

	started := time.Now()
	err := p.push.RequestPosition(ctx, current.trip.TripID, current.trip.DriverID)
	result := resultSuccess
	if err != nil {
		result = resultError
		span.RecordError(err)
		span.SetStatus(codes.Error, "push request failed")
		p.logger.WarnContext(ctx, "push service request failed", "trip_id", current.trip.TripID.String(), "error", err)
	}
	p.metrics.requests.Add(ctx, 1, resultAttr(result))
	p.metrics.duration.Record(ctx, time.Since(started).Seconds(), resultAttr(result))
}
