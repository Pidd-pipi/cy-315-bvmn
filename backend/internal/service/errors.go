package service

import "errors"

var (
	// ErrNotFound indicates a requested record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalid indicates malformed business input.
	ErrInvalid = errors.New("invalid input")
	// ErrConflict indicates a constraint violation.
	ErrConflict = errors.New("resource conflict")
	// ErrDraftNameExists indicates a schedule draft with the same name already exists.
	ErrDraftNameExists = errors.New("draft name already exists")
	// ErrDraftAlreadyPublished indicates the schedule draft was already published.
	ErrDraftAlreadyPublished = errors.New("draft already published")
	// ErrDraftHasConflicts indicates the schedule draft failed the pre-publish conflict check.
	ErrDraftHasConflicts = errors.New("draft has scheduling conflicts")
)
