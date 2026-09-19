package repository

import "errors"

var (
	// ErrNotFound is returned when a requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConstraint is returned when a database constraint rejects a write.
	ErrConstraint = errors.New("constraint violation")
	// ErrAlreadyPublished is returned when an operation targets a draft that
	// has already been published.
	ErrAlreadyPublished = errors.New("draft already published")
	// ErrConcurrentPublish is returned when another transaction wins the
	// single-writer claim on a draft (rows affected == 0).
	ErrConcurrentPublish = errors.New("concurrent publish")
)
