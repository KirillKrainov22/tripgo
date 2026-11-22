package trip

import (
	"errors"
	"fmt"
)

var (
	ErrTripNotFound        = errors.New("trip not found")
	ErrTripCompleted       = errors.New("trip already completed")
	ErrDriverBusy          = errors.New("driver already has an active trip")
	ErrIdempotencyConflict = errors.New("idempotency key was used with different trip data")
)

type ValidationError struct {
	Reason string
}

func NewValidationError(format string, args ...any) error {
	return &ValidationError{Reason: fmt.Sprintf(format, args...)}
}

func (e *ValidationError) Error() string {
	return "invalid request: " + e.Reason
}
