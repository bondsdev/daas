package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// defaultBaseURL is OpenRouteService's current documented API domain.
// Overridable via NewORSClient's baseURL argument in case ORS's documented
// migration to api.heigit.org becomes necessary later.
const defaultBaseURL = "https://api.openrouteservice.org"

// directionsPath uses the geojson variant: it returns a GeoJSON
// FeatureCollection directly, which Leaflet consumes natively via
// L.geoJSON(...) with no extra polyline-decoding dependency.
const directionsPath = "/v2/directions/driving-car/geojson"

// ORSClient calls OpenRouteService's Directions API. It must only ever be
// constructed and used server-side — the API key it holds must never reach
// the browser bundle or be logged.
type ORSClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewORSClient builds an ORSClient. Pass "" for baseURL to use ORS's
// default domain.
func NewORSClient(baseURL, apiKey string) *ORSClient {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &ORSClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type orsDirectionsRequest struct {
	Coordinates [][2]float64 `json:"coordinates"`
}

type orsGeoJSONResponse struct {
	Features []orsFeature `json:"features"`
}

type orsFeature struct {
	Geometry   orsGeometry   `json:"geometry"`
	Properties orsProperties `json:"properties"`
}

type orsGeometry struct {
	Coordinates [][2]float64 `json:"coordinates"`
}

type orsProperties struct {
	Summary orsSummary `json:"summary"`
}

type orsSummary struct {
	Distance float64 `json:"distance"`
	Duration float64 `json:"duration"`
}

// GetRoute requests a driving route through the given waypoints, in order
// (caller decides stop order — this client does no optimization), and
// returns the parsed line geometry plus distance/duration. Requires at
// least 2 waypoints, same as the ORS API itself.
func (c *ORSClient) GetRoute(ctx context.Context, waypoints []Waypoint) (*Route, error) {
	if len(waypoints) < 2 {
		return nil, fmt.Errorf("ors client: need at least 2 waypoints, got %d", len(waypoints))
	}

	coords := make([][2]float64, len(waypoints))
	for i, wp := range waypoints {
		// ORS/GeoJSON convention is [longitude, latitude] — the opposite of
		// this codebase's lat/lng naming everywhere else. Get this wrong and
		// every route silently points at the wrong hemisphere.
		coords[i] = [2]float64{wp.Lng, wp.Lat}
	}

	reqBody, err := json.Marshal(orsDirectionsRequest{Coordinates: coords})
	if err != nil {
		return nil, fmt.Errorf("ors client: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+directionsPath, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("ors client: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// ORS accepts the key as-is in the Authorization header — no "Bearer"
	// prefix, no JWT decoding. Verified against the live API.
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ors client: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ors client: unexpected status %d", resp.StatusCode)
	}

	var parsed orsGeoJSONResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("ors client: decode response: %w", err)
	}

	if len(parsed.Features) == 0 || len(parsed.Features[0].Geometry.Coordinates) == 0 {
		return nil, fmt.Errorf("ors client: response has no usable route geometry")
	}

	feature := parsed.Features[0]
	return &Route{
		Coordinates:     feature.Geometry.Coordinates,
		DistanceMeters:  feature.Properties.Summary.Distance,
		DurationSeconds: feature.Properties.Summary.Duration,
	}, nil
}
