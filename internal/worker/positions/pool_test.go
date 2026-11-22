package positions_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/KirillKrainov22/tripgo/internal/model"
	"github.com/KirillKrainov22/tripgo/internal/worker/positions"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type fakeTicker struct {
	ticks chan time.Time
}

func (f *fakeTicker) Chan() <-chan time.Time {
	return f.ticks
}

func (f *fakeTicker) Stop() {}

type harness struct {
	pool   *positions.Pool
	finder *MockstaleTripFinder
	push   *MockpushGateway
	ticker *fakeTicker
	reader *sdkmetric.ManualReader
}

func newHarness(t *testing.T, workers, queueSize int) *harness {
	t.Helper()
	ctrl := gomock.NewController(t)
	h := &harness{
		finder: NewMockstaleTripFinder(ctrl),
		push:   NewMockpushGateway(ctrl),
		ticker: &fakeTicker{ticks: make(chan time.Time)},
		reader: sdkmetric.NewManualReader(),
	}
	pool, err := positions.New(positions.Config{
		CheckInterval:  30 * time.Second,
		StaleAfter:     time.Minute,
		Workers:        workers,
		QueueSize:      queueSize,
		RequestTimeout: time.Second,
	},
		h.finder,
		h.push,
		func(time.Duration) positions.Ticker { return h.ticker },
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(h.reader)).Meter("test"),
		noop.NewTracerProvider(),
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	h.pool = pool
	return h
}

func (h *harness) tick() {
	h.ticker.ticks <- time.Now()
}

func (h *harness) stop(t *testing.T) {
	t.Helper()
	h.pool.StopScanner()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.pool.StopWorkers(ctx); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) count(t *testing.T, name, result string) int64 {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != name {
				continue
			}
			sum, ok := item.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is not a counter", name)
			}
			for _, point := range sum.DataPoints {
				if value, _ := point.Attributes.Value(attribute.Key("result")); value.AsString() == result {
					return point.Value
				}
			}
		}
	}
	return 0
}

func staleTrip() model.StaleTrip {
	return model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New(), LastSeenAt: time.Now().Add(-time.Hour)}
}

func (h *harness) blockFirstWorker(t *testing.T) (release func()) {
	t.Helper()
	busy := staleTrip()
	started := make(chan struct{})
	unblock := make(chan struct{})
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return([]model.StaleTrip{busy}, nil)
	h.push.EXPECT().RequestPosition(gomock.Any(), busy.TripID, busy.DriverID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(started)
			<-unblock
			return nil
		})
	h.tick()
	<-started
	return func() { close(unblock) }
}

func TestStaleTripGetsPositionRequest(t *testing.T) {
	h := newHarness(t, 2, 10)
	h.pool.Start(context.Background())
	trip := staleTrip()
	done := make(chan struct{})
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return([]model.StaleTrip{trip}, nil)
	h.push.EXPECT().RequestPosition(gomock.Any(), trip.TripID, trip.DriverID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(done)
			return nil
		})

	h.tick()
	<-done
	h.stop(t)

	if got := h.count(t, "tripgo.stale_checks", "queued"); got != 1 {
		t.Fatalf("queued = %d, want 1", got)
	}
	if got := h.count(t, "tripgo.position_requests", "success"); got != 1 {
		t.Fatalf("success = %d, want 1", got)
	}
}

func TestQueuedTripIsNotAddedAgain(t *testing.T) {
	h := newHarness(t, 1, 10)
	h.pool.Start(context.Background())
	release := h.blockFirstWorker(t)
	waiting := staleTrip()
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return([]model.StaleTrip{waiting}, nil).Times(2)
	h.push.EXPECT().RequestPosition(gomock.Any(), waiting.TripID, waiting.DriverID).Return(nil).Times(1)

	h.tick()
	h.tick()
	h.pool.StopScanner()
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.pool.StopWorkers(ctx); err != nil {
		t.Fatal(err)
	}

	if got := h.count(t, "tripgo.stale_checks", "duplicate"); got != 1 {
		t.Fatalf("duplicate = %d, want 1", got)
	}
	if got := h.count(t, "tripgo.stale_checks", "queued"); got != 2 {
		t.Fatalf("queued = %d, want 2", got)
	}
}

func TestPositionCancelsQueuedRequest(t *testing.T) {
	h := newHarness(t, 1, 10)
	h.pool.Start(context.Background())
	release := h.blockFirstWorker(t)
	canceled := staleTrip()
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return([]model.StaleTrip{canceled}, nil)

	h.tick()
	h.pool.StopScanner()
	h.pool.Cancel(canceled.TripID)
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.pool.StopWorkers(ctx); err != nil {
		t.Fatal(err)
	}

	if got := h.count(t, "tripgo.position_requests", "success"); got != 1 {
		t.Fatalf("success = %d, want only the busy trip", got)
	}
}

func TestFullQueueDropsTasksWithoutBlocking(t *testing.T) {
	h := newHarness(t, 1, 1)
	h.pool.Start(context.Background())
	release := h.blockFirstWorker(t)
	first, second, third := staleTrip(), staleTrip(), staleTrip()
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return([]model.StaleTrip{first, second, third}, nil)
	h.push.EXPECT().RequestPosition(gomock.Any(), first.TripID, first.DriverID).Return(nil)

	h.tick()
	h.pool.StopScanner()
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.pool.StopWorkers(ctx); err != nil {
		t.Fatal(err)
	}

	if got := h.count(t, "tripgo.stale_checks", "dropped"); got != 2 {
		t.Fatalf("stale_checks dropped = %d, want 2", got)
	}
	if got := h.count(t, "tripgo.position_requests", "dropped"); got != 2 {
		t.Fatalf("position_requests dropped = %d, want 2", got)
	}
}

func TestWorkersProcessTasksInParallel(t *testing.T) {
	const workers = 3
	h := newHarness(t, workers, 10)
	h.pool.Start(context.Background())
	trips := []model.StaleTrip{staleTrip(), staleTrip(), staleTrip(), staleTrip(), staleTrip(), staleTrip()}
	started := make(chan struct{}, len(trips))
	release := make(chan struct{})
	var running, maxRunning atomic.Int64
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return(trips, nil)
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			now := running.Add(1)
			for {
				peak := maxRunning.Load()
				if now <= peak || maxRunning.CompareAndSwap(peak, now) {
					break
				}
			}
			started <- struct{}{}
			<-release
			running.Add(-1)
			return nil
		}).Times(len(trips))

	h.tick()
	for range workers {
		<-started
	}
	close(release)
	h.stop(t)

	if got := maxRunning.Load(); got != workers {
		t.Fatalf("max parallel requests = %d, want %d", got, workers)
	}
	if got := h.count(t, "tripgo.position_requests", "success"); got != int64(len(trips)) {
		t.Fatalf("success = %d, want %d", got, len(trips))
	}
}

func TestPushErrorDoesNotStopWorker(t *testing.T) {
	h := newHarness(t, 1, 10)
	h.pool.Start(context.Background())
	failing, next := staleTrip(), staleTrip()
	done := make(chan struct{})
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return([]model.StaleTrip{failing, next}, nil)
	h.push.EXPECT().RequestPosition(gomock.Any(), failing.TripID, failing.DriverID).Return(errors.New("push service unavailable"))
	h.push.EXPECT().RequestPosition(gomock.Any(), next.TripID, next.DriverID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(done)
			return nil
		})

	h.tick()
	<-done
	h.stop(t)

	if got := h.count(t, "tripgo.position_requests", "error"); got != 1 {
		t.Fatalf("error = %d, want 1", got)
	}
	if got := h.count(t, "tripgo.position_requests", "success"); got != 1 {
		t.Fatalf("success = %d, want 1", got)
	}
}

func TestScanErrorKeepsScannerRunning(t *testing.T) {
	h := newHarness(t, 1, 10)
	h.pool.Start(context.Background())
	trip := staleTrip()
	done := make(chan struct{})
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return(nil, errors.New("database is unavailable"))
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return([]model.StaleTrip{trip}, nil)
	h.push.EXPECT().RequestPosition(gomock.Any(), trip.TripID, trip.DriverID).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			close(done)
			return nil
		})

	h.tick()
	h.tick()
	<-done
	h.stop(t)
}

func TestStopFinishesQueuedTasks(t *testing.T) {
	h := newHarness(t, 1, 10)
	h.pool.Start(context.Background())
	release := h.blockFirstWorker(t)
	first, second := staleTrip(), staleTrip()
	var processed atomic.Int64
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return([]model.StaleTrip{first, second}, nil)
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			processed.Add(1)
			return nil
		}).Times(2)

	h.tick()
	h.pool.StopScanner()
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.pool.StopWorkers(ctx); err != nil {
		t.Fatal(err)
	}

	if got := processed.Load(); got != 2 {
		t.Fatalf("processed queued tasks = %d, want 2", got)
	}
}

func TestStopWorkersGivesUpAfterDeadline(t *testing.T) {
	h := newHarness(t, 1, 10)
	h.pool.Start(context.Background())
	stuck := staleTrip()
	started := make(chan struct{})
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute).Return([]model.StaleTrip{stuck}, nil)
	h.push.EXPECT().RequestPosition(gomock.Any(), stuck.TripID, stuck.DriverID).
		DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})

	h.tick()
	<-started
	h.pool.StopScanner()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := h.pool.StopWorkers(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestContextCancelStopsScannerAndWorkers(t *testing.T) {
	ignore := goleak.IgnoreCurrent()
	h := newHarness(t, 3, 10)
	ctx, cancel := context.WithCancel(context.Background())
	h.pool.Start(ctx)

	cancel()

	goleak.VerifyNone(t, ignore)
}
