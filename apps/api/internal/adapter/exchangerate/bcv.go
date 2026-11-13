package exchangerate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// defaultBCVURL points at a public aggregator of the official BCV (Banco
// Central de Venezuela) BS/USD rate. Response shape varies by field naming
// across similar public endpoints, so BCVFetcher reads it defensively.
const defaultBCVURL = "https://ve.dolarapi.com/v1/dolares/oficial"

// rateFieldPriority lists candidate JSON field names for the rate value, in
// the order they're tried. Covers the dolarapi.com shape ("promedio",
// "venta", "compra") plus common generic names other aggregators use.
var rateFieldPriority = []string{"promedio", "venta", "compra", "rate", "price"}

// BCVFetcher fetches the official BS/USD rate from a public HTTP endpoint.
// On any error (network, timeout, malformed response, missing/invalid field)
// it returns an error; the caller falls back to a manual rate.
type BCVFetcher struct {
	url        string
	httpClient *http.Client
}

// NewBCVFetcher builds a BCVFetcher with a 5s timeout against the given URL.
// Pass "" to use the default public endpoint.
func NewBCVFetcher(url string) *BCVFetcher {
	if url == "" {
		url = defaultBCVURL
	}
	return &BCVFetcher{
		url:        url,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (f *BCVFetcher) FetchRate(ctx context.Context) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return 0, fmt.Errorf("bcv fetcher: build request: %w", err)
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("bcv fetcher: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bcv fetcher: status %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("bcv fetcher: decode response: %w", err)
	}

	for _, field := range rateFieldPriority {
		v, ok := body[field]
		if !ok {
			continue
		}
		n, ok := v.(float64)
		if !ok || n <= 0 {
			continue
		}
		return n, nil
	}
	return 0, fmt.Errorf("bcv fetcher: no usable rate field in response")
}
