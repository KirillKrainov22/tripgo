package positions_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
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

const batchSize = 100

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
		BatchSize:      batchSize,
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
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{busy}, nil)
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
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{trip}, nil)
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
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{waiting}, nil).Times(2)
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
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{canceled}, nil)

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

func TestPositionCancelsTripReadByConcurrentScan(t *testing.T) {
	for _, alreadyQueued := range []bool{false, true} {
		name := "not yet queued"
		if alreadyQueued {
			name = "already queued"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, 1, 10)
			h.pool.Start(context.Background())
			release := h.blockFirstWorker(t)
			canceled, unrelated := staleTrip(), staleTrip()
			if alreadyQueued {
				h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).
					Return([]model.StaleTrip{canceled}, nil)
				h.tick()
			}
			h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).
				DoAndReturn(func(context.Context, time.Duration, *model.StaleTrip, int) ([]model.StaleTrip, error) {
					// A coordinate commits after the query snapshot, before its rows reach the scanner.
					h.pool.Cancel(canceled.TripID)
					return []model.StaleTrip{canceled, unrelated}, nil
				})
			log := &requestLog{}
			h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(log.record).AnyTimes()

			h.tick()
			h.pool.StopScanner()
			release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.pool.StopWorkers(ctx); err != nil {
				t.Fatal(err)
			}

			if len(log.trips) != 1 || log.trips[0] != unrelated.TripID {
				t.Fatalf("requests = %v, want only unrelated trip %s", log.trips, unrelated.TripID)
			}
			if got := h.count(t, "tripgo.stale_checks", "cancelled"); got != 1 {
				t.Fatalf("cancelled = %d, want 1", got)
			}
		})
	}
}

func TestCancellationDoesNotSuppressLaterScan(t *testing.T) {
	for _, scanError := range []error{nil, errors.New("database unavailable")} {
		name := "successful query"
		if scanError != nil {
			name = "failed query"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, 1, 10)
			h.pool.Start(context.Background())
			trip := staleTrip()
			gomock.InOrder(
				h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).
					DoAndReturn(func(context.Context, time.Duration, *model.StaleTrip, int) ([]model.StaleTrip, error) {
						h.pool.Cancel(trip.TripID)
						return []model.StaleTrip{trip}, scanError
					}),
				h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).
					Return([]model.StaleTrip{trip}, nil),
			)
			h.push.EXPECT().RequestPosition(gomock.Any(), trip.TripID, trip.DriverID).Return(nil)

			h.tick()
			h.tick()
			h.stop(t)
		})
	}
}

func TestCancellationOverflowEndsScanWithoutSuppressingNextTick(t *testing.T) {
	for _, distinct := range []bool{true, false} {
		name := "distinct cancellations invalidate scan"
		if !distinct {
			name = "repeated cancellation keeps unrelated trips"
		}
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, 1, 10)
			h.pool.Start(context.Background())
			release := h.blockFirstWorker(t)
			current, next := staleTrip(), staleTrip()
			gomock.InOrder(
				h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).
					DoAndReturn(func(context.Context, time.Duration, *model.StaleTrip, int) ([]model.StaleTrip, error) {
						canceledID := uuid.New()
						for range batchSize + 1 {
							if distinct {
								canceledID = uuid.New()
							}
							h.pool.Cancel(canceledID)
						}
						return []model.StaleTrip{current}, nil
					}),
				h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).
					Return([]model.StaleTrip{next}, nil),
			)
			log := &requestLog{}
			h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(log.record).AnyTimes()

			h.tick()
			h.tick()
			h.pool.StopScanner()
			release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.pool.StopWorkers(ctx); err != nil {
				t.Fatal(err)
			}

			want := []uuid.UUID{next.TripID}
			if !distinct {
				want = []uuid.UUID{current.TripID, next.TripID}
			}
			if len(log.trips) != len(want) {
				t.Fatalf("requests = %v, want %v", log.trips, want)
			}
			for i := range want {
				if log.trips[i] != want[i] {
					t.Fatalf("requests = %v, want %v", log.trips, want)
				}
			}
		})
	}
}

func TestFullQueueDropsTaskAndStopsScan(t *testing.T) {
	h := newHarness(t, 1, 1)
	h.pool.Start(context.Background())
	release := h.blockFirstWorker(t)
	first, second, third := staleTrip(), staleTrip(), staleTrip()
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{first, second, third}, nil)
	h.push.EXPECT().RequestPosition(gomock.Any(), first.TripID, first.DriverID).Return(nil)

	h.tick()
	h.pool.StopScanner()
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.pool.StopWorkers(ctx); err != nil {
		t.Fatal(err)
	}

	if got := h.count(t, "tripgo.stale_checks", "dropped"); got != 1 {
		t.Fatalf("stale_checks dropped = %d, want 1", got)
	}
	if got := h.count(t, "tripgo.position_requests", "dropped"); got != 1 {
		t.Fatalf("position_requests dropped = %d, want 1", got)
	}
	if got := h.count(t, "tripgo.stale_checks", "queued"); got != 2 {
		t.Fatalf("queued = %d, want 2: the third trip must not be checked after the queue is full", got)
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
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return(trips, nil)
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
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{failing, next}, nil)
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
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return(nil, errors.New("database is unavailable"))
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{trip}, nil)
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
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{first, second}, nil)
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
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{stuck}, nil)
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

func staleTrips(count int) []model.StaleTrip {
	trips := make([]model.StaleTrip, 0, count)
	for range count {
		trips = append(trips, staleTrip())
	}
	return trips
}

func (h *harness) histogramSum(t *testing.T, name string) int64 {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, item := range scope.Metrics {
			if histogram, ok := item.Data.(metricdata.Histogram[int64]); ok && item.Name == name {
				var sum int64
				for _, point := range histogram.DataPoints {
					sum += point.Sum
				}
				return sum
			}
		}
	}
	return 0
}

func TestScannerReadsAllBatches(t *testing.T) {
	h := newHarness(t, 4, 300)
	h.pool.Start(context.Background())
	trips := staleTrips(250)
	gomock.InOrder(
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Nil(), batchSize).Return(trips[:100], nil),
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, &trips[99], batchSize).Return(trips[100:200], nil),
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, &trips[199], batchSize).Return(trips[200:], nil),
	)
	var requested atomic.Int64
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			requested.Add(1)
			return nil
		}).Times(len(trips))

	h.tick()
	h.stop(t)

	if got := requested.Load(); got != int64(len(trips)) {
		t.Fatalf("requested = %d, want %d", got, len(trips))
	}
	if got := h.count(t, "tripgo.stale_checks", "queued"); got != int64(len(trips)) {
		t.Fatalf("queued = %d, want %d", got, len(trips))
	}
	if got := h.histogramSum(t, "tripgo.stale_scan.rows"); got != int64(len(trips)) {
		t.Fatalf("rows read = %d, want %d", got, len(trips))
	}
}

func TestScannerStopsReadingWhenQueueIsFull(t *testing.T) {
	h := newHarness(t, 1, 150)
	h.pool.Start(context.Background())
	release := h.blockFirstWorker(t)
	trips := staleTrips(200)
	gomock.InOrder(
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Nil(), batchSize).Return(trips[:100], nil),
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, &trips[99], batchSize).Return(trips[100:], nil),
	)
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(150)

	h.tick()
	h.pool.StopScanner()
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.pool.StopWorkers(ctx); err != nil {
		t.Fatal(err)
	}

	if got := h.count(t, "tripgo.stale_checks", "queued"); got != 151 {
		t.Fatalf("queued = %d, want 151", got)
	}
	if got := h.count(t, "tripgo.stale_checks", "dropped"); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
}

func tripSilentFor(silence time.Duration) model.StaleTrip {
	return model.StaleTrip{TripID: uuid.New(), DriverID: uuid.New(), LastSeenAt: time.Now().Add(-silence)}
}

type requestLog struct {
	mu    sync.Mutex
	trips []uuid.UUID
}

func (l *requestLog) record(_ context.Context, tripID, _ uuid.UUID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.trips = append(l.trips, tripID)
	return nil
}

func TestQueueServesTripsSilentLongerFirst(t *testing.T) {
	h := newHarness(t, 1, 10)
	h.pool.Start(context.Background())
	release := h.blockFirstWorker(t)
	oldest, middle, freshest := tripSilentFor(3*time.Hour), tripSilentFor(2*time.Hour), tripSilentFor(time.Hour)
	h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).
		Return([]model.StaleTrip{middle, freshest, oldest}, nil)
	log := &requestLog{}
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(log.record).Times(3)

	h.tick()
	h.pool.StopScanner()
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.pool.StopWorkers(ctx); err != nil {
		t.Fatal(err)
	}

	want := []uuid.UUID{oldest.TripID, middle.TripID, freshest.TripID}
	if len(log.trips) != len(want) {
		t.Fatalf("requests = %v, want %v", log.trips, want)
	}
	for i := range want {
		if log.trips[i] != want[i] {
			t.Fatalf("request %d went to %s, want %s", i, log.trips[i], want[i])
		}
	}
}

func TestOlderTripEvictsFresherOneWhenQueueIsFull(t *testing.T) {
	h := newHarness(t, 1, 2)
	h.pool.Start(context.Background())
	release := h.blockFirstWorker(t)
	fresh, fresher := tripSilentFor(time.Hour), tripSilentFor(50*time.Minute)
	oldest := tripSilentFor(3 * time.Hour)
	newest := tripSilentFor(10 * time.Minute)
	gomock.InOrder(
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{fresh, fresher}, nil),
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{oldest}, nil),
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Any(), batchSize).Return([]model.StaleTrip{newest}, nil),
	)
	log := &requestLog{}
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(log.record).Times(2)

	h.tick()
	h.tick()
	h.tick()
	h.pool.StopScanner()
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.pool.StopWorkers(ctx); err != nil {
		t.Fatal(err)
	}

	if len(log.trips) != 2 || log.trips[0] != oldest.TripID || log.trips[1] != fresh.TripID {
		t.Fatalf("requests = %v, want oldest %s then fresh %s", log.trips, oldest.TripID, fresh.TripID)
	}
	if got := h.count(t, "tripgo.position_requests", "dropped"); got != 2 {
		t.Fatalf("dropped requests = %d, want 2: evicted fresher trip and rejected newest trip", got)
	}
	if got := h.count(t, "tripgo.stale_checks", "dropped"); got != 1 {
		t.Fatalf("dropped checks = %d, want 1 for the newest trip", got)
	}
}

func TestLateCoordinateDoesNotRepeatPushWithinScan(t *testing.T) {
	h := newHarness(t, 1, 300)
	h.pool.Start(context.Background())
	first, second := staleTrips(batchSize), staleTrips(batchSize)
	base := time.Now().Add(-10 * time.Hour)
	for i := range first {
		first[i].LastSeenAt = base.Add(time.Duration(i) * time.Minute)
		second[i].LastSeenAt = base.Add(time.Duration(batchSize+i) * time.Minute)
	}
	moved := first[0]
	moved.LastSeenAt = base.Add(5 * time.Hour) // A late coordinate advances the cursor key but remains stale.
	firstPush := make(chan struct{})
	var requests atomic.Int64
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, tripID, _ uuid.UUID) error {
			if tripID == moved.TripID && requests.Add(1) == 1 {
				close(firstPush)
			}
			return nil
		}).AnyTimes()
	gomock.InOrder(
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, gomock.Nil(), batchSize).
			Return(first, nil),
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, &first[len(first)-1], batchSize).
			DoAndReturn(func(context.Context, time.Duration, *model.StaleTrip, int) ([]model.StaleTrip, error) {
				<-firstPush
				// The coordinate commits during the second query. Its new last_position_at
				// places the same trip after this page, so a later SQL page returns it again.
				h.pool.Cancel(moved.TripID)
				return second, nil
			}),
		h.finder.EXPECT().FindStale(gomock.Any(), time.Minute, &second[len(second)-1], batchSize).
			Return([]model.StaleTrip{moved}, nil),
	)

	h.tick()
	h.stop(t)

	if got := requests.Load(); got != 1 {
		t.Fatalf("pushes for one trip in one scan = %d, want 1", got)
	}
}
