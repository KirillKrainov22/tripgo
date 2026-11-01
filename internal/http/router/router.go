package router

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/KirillKrainov22/tripgo/internal/domain/trip"
	api "github.com/KirillKrainov22/tripgo/internal/generated"
	"github.com/KirillKrainov22/tripgo/internal/http/domainerror"
	"github.com/KirillKrainov22/tripgo/internal/http/middleware"
	"github.com/KirillKrainov22/tripgo/internal/http/respond"
	tripcreate "github.com/KirillKrainov22/tripgo/internal/http/trip/create"
	tripfinish "github.com/KirillKrainov22/tripgo/internal/http/trip/finish"
	tripget "github.com/KirillKrainov22/tripgo/internal/http/trip/get"
)

type Config struct {
	CreateTrip     *tripcreate.Handler
	GetTrip        *tripget.Handler
	FinishTrip     *tripfinish.Handler
	Ping           func(ctx context.Context) error
	ReadyTimeout   time.Duration
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	Meter          metric.Meter
}

type server struct {
	cfg Config
}

func New(cfg Config) (http.Handler, error) {
	routes := chi.NewRouter()
	routes.NotFound(func(w http.ResponseWriter, r *http.Request) {
		status, body := domainerror.New(http.StatusNotFound, "route_not_found", "Route not found", "The requested route does not exist")
		respond.Problem(w, r, status, body)
	})
	routes.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		status, body := domainerror.New(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "The HTTP method is not supported for this route")
		respond.Problem(w, r, status, body)
	})
	api.HandlerWithOptions(&server{cfg: cfg}, api.ChiServerOptions{
		BaseRouter: routes,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			respond.Error(w, r, cfg.Logger, parameterError(err))
		},
	})

	metrics, err := middleware.Metrics(cfg.Meter, routes)
	if err != nil {
		return nil, err
	}
	// Recover handler panics before unwinding telemetry, so it observes the 500.
	// The outer Recover below remains a safety net for the middleware themselves.
	handler := metrics(middleware.Recover(cfg.Logger)(routes))
	handler = middleware.Logging(cfg.Logger, routes)(handler)
	handler = middleware.Tracing(cfg.TracerProvider, routes)(handler)
	handler = middleware.Recover(cfg.Logger)(handler)
	return handler, nil
}

func parameterError(err error) error {
	var invalidFormat *api.InvalidParamFormatError
	if errors.As(err, &invalidFormat) {
		return trip.NewValidationError("%s must be a UUID", invalidFormat.ParamName)
	}
	var tooMany *api.TooManyValuesForParamError
	if errors.As(err, &tooMany) {
		return trip.NewValidationError("%s must be sent once", tooMany.ParamName)
	}
	return trip.NewValidationError("invalid request parameter")
}

func (s *server) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	s.cfg.CreateTrip.Handle(w, r, params)
}

func (s *server) GetTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	s.cfg.GetTrip.Handle(w, r, id)
}

func (s *server) FinishTrip(w http.ResponseWriter, r *http.Request, id api.TripId) {
	s.cfg.FinishTrip.Handle(w, r, id)
}

func (s *server) Health(w http.ResponseWriter, _ *http.Request) {
	respond.JSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (s *server) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.ReadyTimeout)
	defer cancel()
	if err := s.cfg.Ping(ctx); err != nil {
		s.cfg.Logger.WarnContext(r.Context(), "readiness check failed", "error", err)
		respond.JSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}
	respond.JSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}
