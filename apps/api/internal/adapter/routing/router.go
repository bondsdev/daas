// Package routing provides a server-side adapter to an external driving
// directions provider (OpenRouteService), used to build the rider's
// multi-stop delivery route. The API key this adapter sends never reaches
// the browser — only the Go backend calls out to the routing provider.
package routing

import "context"

// Waypoint is a point to route through, expressed as plain latitude/
// longitude — the same axis order this codebase's delivery_lat/delivery_lng
// columns use everywhere else. The ORS/GeoJSON wire format wants the
// opposite ([lng, lat]) order; that swap happens inside the adapter, never
// at the call site.
type Waypoint struct {
	Lat float64
	Lng float64
}

// Route is a parsed driving route: an ordered line geometry (as [lng, lat]
// pairs, GeoJSON/ORS convention, so callers can feed it straight to
// Leaflet's L.geoJSON) plus total distance and duration.
type Route struct {
	// Coordinates is the route line geometry, each pair as [lng, lat].
	Coordinates     [][2]float64 `json:"coordinates"`
	DistanceMeters  float64      `json:"distance_meters"`
	DurationSeconds float64      `json:"duration_seconds"`
}

// Provider calls out to a driving-directions API for an ordered set of
// waypoints. Implemented by ORSClient; app.RiderService depends on this
// interface (not the concrete client) so it can be exercised with a fake
// in tests, mirroring this codebase's exchangerate.Fetcher pattern.
type Provider interface {
	GetRoute(ctx context.Context, waypoints []Waypoint) (*Route, error)
}
