package service

import (
	"errors"

	"github.com/gbschedule/gbschedule/internal/dto"
)

var (
	// ErrNotFound indicates a requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalid indicates malformed business input.
	ErrInvalid = errors.New("invalid input")
	// ErrConflict indicates a constraint violation.
	ErrConflict = errors.New("resource conflict")
)

// DetailError pairs a sentinel error with a user-facing explanation so
// handlers can surface a specific failure reason without leaking internals.
type DetailError struct {
	Sentinel error
	Detail   string
}

// NewDetailError wraps sentinel with a human-readable detail.
func NewDetailError(sentinel error, detail string) *DetailError {
	return &DetailError{Sentinel: sentinel, Detail: detail}
}

func (e *DetailError) Error() string { return e.Detail }

func (e *DetailError) Unwrap() error { return e.Sentinel }

// ScheduleConflictsError reports that a draft snapshot contains timetable
// conflicts, so publication must be rejected.
type ScheduleConflictsError struct {
	Conflicts []dto.ConflictResponse
}

func (e *ScheduleConflictsError) Error() string {
	return "draft contains schedule conflicts"
}

func (e *ScheduleConflictsError) Unwrap() error { return ErrConflict }
