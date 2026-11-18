package routing

import "context"

// MockProvider is a test double for Provider, mirroring this codebase's
// exchangerate.MockFetcher pattern.
type MockProvider struct {
	Route *Route
	Err   error
}

func (m *MockProvider) GetRoute(_ context.Context, _ []Waypoint) (*Route, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Route, nil
}
