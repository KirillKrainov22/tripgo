package positions

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	resultQueued    = "queued"
	resultDuplicate = "duplicate"
	resultDropped   = "dropped"
	resultSuccess   = "success"
	resultError     = "error"
)

type metrics struct {
	staleChecks metric.Int64Counter
	requests    metric.Int64Counter
	duration    metric.Float64Histogram
}

func newMetrics(meter metric.Meter, queueSize, activeWorkers func() int64) (*metrics, error) {
	staleChecks, err := meter.Int64Counter("tripgo.stale_checks",
		metric.WithUnit("{trip}"), metric.WithDescription("What the scanner did with a stale trip"))
	if err != nil {
		return nil, err
	}
	requests, err := meter.Int64Counter("tripgo.position_requests",
		metric.WithUnit("{request}"), metric.WithDescription("Results of position requests"))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("tripgo.position_requests.duration",
		metric.WithUnit("s"), metric.WithDescription("Duration of Push Service calls"),
		metric.WithExplicitBucketBoundaries(0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5))
	if err != nil {
		return nil, err
	}
	_, err = meter.Int64ObservableGauge("tripgo.position_requests.queue.size",
		metric.WithUnit("{task}"), metric.WithDescription("Tasks waiting in the queue"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			observer.Observe(queueSize())
			return nil
		}))
	if err != nil {
		return nil, err
	}
	_, err = meter.Int64ObservableGauge("tripgo.position_requests.workers.active",
		metric.WithUnit("{worker}"), metric.WithDescription("Workers busy with a task"),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			observer.Observe(activeWorkers())
			return nil
		}))
	if err != nil {
		return nil, err
	}
	return &metrics{staleChecks: staleChecks, requests: requests, duration: duration}, nil
}

func resultAttr(result string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("result", result))
}
