package repository

import "errors"

var (
	// ErrNotFound is returned when a requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConstraint is returned when a database constraint rejects a write.
	ErrConstraint = errors.New("constraint violation")
	// ErrAlreadyPublished is returned when a draft publish loses the atomic
	// compare-and-swap because the draft was already published.
	ErrAlreadyPublished = errors.New("already published")
)
