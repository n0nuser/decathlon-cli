package api

import (
	"fmt"
	"time"
)

// Kind classifies a failure so the CLI can pick an exit code and a remedy.
type Kind int

const (
	KindGeneric Kind = iota
	KindUsage
	KindNotFound
	KindBlocked
	KindNetwork
	KindAPIChanged
)

// ExitCode follows the documented contract: 2 usage, 4 not found, 5 blocked
// or rate limited, 6 network, 7 the storefront changed shape.
func (k Kind) ExitCode() int {
	switch k {
	case KindUsage:
		return 2
	case KindNotFound:
		return 4
	case KindBlocked:
		return 5
	case KindNetwork:
		return 6
	case KindAPIChanged:
		return 7
	}
	return 1
}

func (k Kind) String() string {
	switch k {
	case KindUsage:
		return "usage"
	case KindNotFound:
		return "not_found"
	case KindBlocked:
		return "blocked"
	case KindNetwork:
		return "network"
	case KindAPIChanged:
		return "api_changed"
	}
	return "generic"
}

// Error is every failure the api package reports on purpose.
type Error struct {
	Kind      Kind
	Msg       string
	Status    int
	Endpoint  string // path only, never the query: search terms stay private
	Body      string // short, for the issue form
	Retryable bool
	// RetryAfter is the wait a 429 asked for, when the site sent a
	// Retry-After header. Zero when it did not.
	RetryAfter time.Duration
	// Landing marks a section page with no product list. It is still a
	// usage error (exit 2), but `category show` falls back to the menu.
	Landing bool
}

func (e *Error) Error() string { return e.Msg }

func apiChanged(format string, args ...any) error {
	return &Error{Kind: KindAPIChanged, Msg: fmt.Sprintf(format, args...)}
}

// Usage is a caller mistake detected while resolving input against live data
// (an unknown filter value, a store that does not exist).
func Usage(format string, args ...any) error {
	return &Error{Kind: KindUsage, Msg: fmt.Sprintf(format, args...)}
}

func notFound(format string, args ...any) error {
	return &Error{Kind: KindNotFound, Msg: fmt.Sprintf(format, args...)}
}
