package exchangerate

import "context"

// MockFetcher returns a fixed rate, or Err when set. Used in tests so they
// never depend on network access.
type MockFetcher struct {
	Rate float64
	Err  error
}

func (m *MockFetcher) FetchRate(_ context.Context) (float64, error) {
	if m.Err != nil {
		return 0, m.Err
	}
	return m.Rate, nil
}
