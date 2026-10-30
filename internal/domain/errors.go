// Package domain holds the backend-independent model of Things data
// (tasks, projects, areas, tags, headings, checklist items) and the
// validation rules every backend must respect.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors shared by every backend. Callers test them with errors.Is;
// the MCP layer maps them to user-facing messages without leaking internals.
var (
	// ErrNotFound means the referenced item does not exist.
	ErrNotFound = errors.New("not found")
	// ErrValidation means the input breaks a domain rule.
	ErrValidation = errors.New("invalid input")
	// ErrUnsupported means the backend cannot perform this operation.
	ErrUnsupported = errors.New("not supported by this backend")
	// ErrUnavailable means the backend cannot be reached (app closed, network…).
	ErrUnavailable = errors.New("backend unavailable")
)

// Invalid builds a validation error for a given field.
func Invalid(field, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrValidation, field, fmt.Sprintf(format, args...))
}

// NotFound builds a not-found error for a given kind of item.
func NotFound(kind, id string) error {
	return fmt.Errorf("%w: %s %q", ErrNotFound, kind, id)
}
