package positions

import (
	"container/heap"
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
	BatchSize      int
	RequestTimeout time.Duration
}

type task struct {
	trip  model.StaleTrip
	token uint64
	link  trace.SpanContext
}

type displacedTask struct {
	tripID uuid.UUID
	result string
}

const resultInvalidated = "invalidated"

type Pool struct {
	cfg       Config
	trips     staleTripFinder
	push      pushGateway
	newTicker func(time.Duration) Ticker
	metrics   *metrics
	tracer    trace.Tracer
	logger    *slog.Logger

	tasks     taskHeap
	ready     chan struct{}
	mu        sync.Mutex
	pending   map[uuid.UUID]uint64
	nextToken uint64
	active    atomic.Int64

	scanCancellations map[uuid.UUID]struct{}
	scanInvalidated   bool

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
		ready:     make(chan struct{}, cfg.QueueSize),
		pending:   make(map[uuid.UUID]uint64),
		scanDone:  make(chan struct{}),
	}
	m, err := newMetrics(meter,
		func() int64 { return int64(len(p.ready)) },
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
	close(p.ready)
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
	if p.scanCancellations == nil || p.scanInvalidated {
		return
	}
	if _, canceled := p.scanCancellations[tripID]; canceled {
		return
	}
	if len(p.scanCancellations) == p.cfg.BatchSize {
		p.scanInvalidated = true
		return
	}
	p.scanCancellations[tripID] = struct{}{}
}

func (p *Pool) beginScan() {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Keep cancellations across pages: a late coordinate can move a trip past
	// the pagination cursor and make a later query return the same trip again.
	p.scanCancellations = make(map[uuid.UUID]struct{})
	p.scanInvalidated = false
}

func (p *Pool) endScan() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scanCancellations = nil
	p.scanInvalidated = false
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
	p.beginScan()
	defer p.endScan()
	rows := 0
	defer func() {
		p.metrics.scanRows.Record(ctx, int64(rows))
		span.SetAttributes(attribute.Int("tripgo.stale_trips", rows))
	}()
	var after *model.StaleTrip
	for {
		batch, err := p.trips.FindStale(ctx, p.cfg.StaleAfter, after, p.cfg.BatchSize)
		if err != nil {
			if ctx.Err() == nil {
				p.logger.ErrorContext(ctx, "stale trips scan failed", "error", err)
				span.RecordError(err)
				span.SetStatus(codes.Error, "scan failed")
			}
			return
		}
		rows += len(batch)
		if !p.enqueueBatch(ctx, batch, span.SpanContext()) || len(batch) < p.cfg.BatchSize {
			return
		}
		after = &batch[len(batch)-1]
	}
}

func (p *Pool) enqueueBatch(ctx context.Context, batch []model.StaleTrip, link trace.SpanContext) bool {
	for _, item := range batch {
		result, displaced := p.enqueue(item, link)
		if result == resultInvalidated {
			p.logger.WarnContext(ctx, "position scan invalidated: cancellation limit reached")
			return false
		}
		p.metrics.staleChecks.Add(ctx, 1, resultAttr(result))
		if displaced != nil {
			p.metrics.requests.Add(ctx, 1, resultAttr(displaced.result))
			if displaced.result == resultDropped {
				p.metrics.staleChecks.Add(ctx, 1, resultAttr(resultDropped))
				p.logger.WarnContext(ctx, "position request evicted by a trip silent for longer", "trip_id", displaced.tripID.String())
			}
		}
		if result == resultDropped {
			p.metrics.requests.Add(ctx, 1, resultAttr(resultDropped))
			p.logger.WarnContext(ctx, "position request dropped: queue is full", "trip_id", item.TripID.String())
			return false
		}
	}
	return true
}

func (p *Pool) enqueue(item model.StaleTrip, link trace.SpanContext) (string, *displacedTask) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.scanInvalidated {
		return resultInvalidated, nil
	}
	if _, canceled := p.scanCancellations[item.TripID]; canceled {
		return resultCancelled, nil
	}
	if _, ok := p.pending[item.TripID]; ok {
		return resultDuplicate, nil
	}
	p.nextToken++
	next := task{trip: item, token: p.nextToken, link: link}
	if len(p.tasks) < p.cfg.QueueSize {
		heap.Push(&p.tasks, next)
		p.pending[item.TripID] = next.token
		p.ready <- struct{}{}
		return resultQueued, nil
	}
	victim := p.victimIndex()
	if p.isPendingLocked(p.tasks[victim]) && !item.LastSeenAt.Before(p.tasks[victim].trip.LastSeenAt) {
		return resultDropped, nil
	}
	evicted := p.tasks[victim]
	heap.Remove(&p.tasks, victim)
	heap.Push(&p.tasks, next)
	p.pending[item.TripID] = next.token
	displaced := &displacedTask{tripID: evicted.trip.TripID, result: resultCancelled}
	if p.isPendingLocked(evicted) {
		delete(p.pending, evicted.trip.TripID)
		displaced.result = resultDropped
	}
	return resultQueued, displaced
}

func (p *Pool) victimIndex() int {
	victim := 0
	for i, candidate := range p.tasks {
		if !p.isPendingLocked(candidate) {
			return i
		}
		if candidate.trip.LastSeenAt.After(p.tasks[victim].trip.LastSeenAt) {
			victim = i
		}
	}
	return victim
}

func (p *Pool) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-p.ready:
			if !ok {
				return
			}
			if current, pending := p.next(); pending {
				p.process(ctx, current)
				p.forget(current)
			} else {
				p.metrics.requests.Add(ctx, 1, resultAttr(resultCancelled))
			}
		}
	}
}

func (p *Pool) next() (task, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	current := p.tasks[0]
	heap.Pop(&p.tasks)
	return current, p.isPendingLocked(current)
}

func (p *Pool) isPendingLocked(current task) bool {
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
