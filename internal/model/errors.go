package model

import "errors"

// Stable error classes are mapped to process exit codes by the CLI. Callers may
// wrap them with context and classify them with errors.Is.
var (
	ErrInvalid  = errors.New("invalid input")
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)
