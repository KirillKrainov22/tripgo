package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/metric"
)

type TripMetrics struct {
	created   metric.Int64Counter
	completed metric.Int64Counter
}

func NewTripMetrics(meter metric.Meter) (*TripMetrics, error) {
	created, err := meter.Int64Counter("tripgo.trips.created", metric.WithUnit("{trip}"), metric.WithDescription("Created trips"))
	if err != nil {
		return nil, err
	}
	completed, err := meter.Int64Counter("tripgo.trips.completed", metric.WithUnit("{trip}"), metric.WithDescription("Completed trips"))
	if err != nil {
		return nil, err
	}
	return &TripMetrics{created: created, completed: completed}, nil
}

func (m *TripMetrics) TripCreated(ctx context.Context) {
	m.created.Add(ctx, 1)
}

func (m *TripMetrics) TripCompleted(ctx context.Context) {
	m.completed.Add(ctx, 1)
}

func RegisterActiveTrips(meter metric.Meter, count func(ctx context.Context) (int64, error)) error {
	_, err := meter.Int64ObservableGauge("tripgo.trips.active",
		metric.WithUnit("{trip}"),
		metric.WithDescription("Trips in active status"),
		metric.WithInt64Callback(func(ctx context.Context, observer metric.Int64Observer) error {
			value, err := count(ctx)
			if err != nil {
				return err
			}
			observer.Observe(value)
			return nil
		}),
	)
	return err
}
