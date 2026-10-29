package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
)

type queryInfoKey struct{}

type queryInfo struct {
	started    time.Time
	operation  string
	collection string
}

type queryTracer struct {
	spans    *otelpgx.Tracer
	duration metric.Float64Histogram
}

func newQueryTracer(tracerProvider trace.TracerProvider, meter metric.Meter) (*queryTracer, error) {
	duration, err := meter.Float64Histogram(
		"db.client.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of database client operations"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		return nil, err
	}
	spans := otelpgx.NewTracer(
		otelpgx.WithTracerProvider(tracerProvider),
		otelpgx.WithMeterProvider(noop.NewMeterProvider()),
		otelpgx.WithDisableConnectionDetailsInAttributes(),
	)
	return &queryTracer{spans: spans, duration: duration}, nil
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	info := queryInfo{
		started:    time.Now(),
		operation:  operationName(data.SQL),
		collection: collectionName(data.SQL),
	}
	ctx = t.spans.TraceQueryStart(ctx, conn, data)
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("db.operation.name", info.operation),
		attribute.String("db.collection.name", info.collection),
	)
	return context.WithValue(ctx, queryInfoKey{}, info)
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	t.spans.TraceQueryEnd(ctx, conn, data)
	info, ok := ctx.Value(queryInfoKey{}).(queryInfo)
	if !ok {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("db.operation.name", info.operation),
		attribute.String("db.collection.name", info.collection),
	}
	if data.Err != nil {
		attrs = append(attrs, attribute.String("error.type", errorType(data.Err)))
	}
	t.duration.Record(ctx, time.Since(info.started).Seconds(), metric.WithAttributes(attrs...))
}

func operationName(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "UNKNOWN"
	}
	return strings.ToUpper(fields[0])
}

func collectionName(sql string) string {
	fields := strings.Fields(sql)
	for i := 0; i < len(fields)-1; i++ {
		switch strings.ToUpper(fields[i]) {
		case "FROM", "INTO", "UPDATE":
			return strings.Trim(fields[i+1], "(),;")
		}
	}
	return "unknown"
}

func errorType(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "no_rows"
	}
	return "error"
}
