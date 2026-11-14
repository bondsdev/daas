// Package exchangerate provides the BS/USD exchange rate fetcher interface
// and its implementations. The interface decouples CashSessionService from the
// physical HTTP source, so a mock can stand in for tests and the manual-rate
// fallback path never depends on network access.
package exchangerate

import "context"

// Fetcher abstracts fetching today's official BS/USD exchange rate.
// Implementations must return quickly and never panic: on any failure the
// caller falls back to a manual rate.
type Fetcher interface {
	FetchRate(ctx context.Context) (rate float64, err error)
}
