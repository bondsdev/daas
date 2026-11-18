package routing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestORSClient_GetRoute_Success(t *testing.T) {
	var gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"features": [
				{
					"geometry": {
						"coordinates": [[-66.9036, 10.4806], [-66.90, 10.48], [-66.89, 10.47]]
					},
					"properties": {
						"summary": {"distance": 1234.5, "duration": 300.0}
					}
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewORSClient(server.URL, "test-key")
	route, err := client.GetRoute(context.Background(), []Waypoint{
		{Lat: 10.4806, Lng: -66.9036},
		{Lat: 10.47, Lng: -66.89},
	})
	if err != nil {
		t.Fatalf("GetRoute returned error: %v", err)
	}
	if route == nil {
		t.Fatal("expected a non-nil route")
	}
	if route.DistanceMeters != 1234.5 {
		t.Errorf("DistanceMeters = %v, want 1234.5", route.DistanceMeters)
	}
	if route.DurationSeconds != 300.0 {
		t.Errorf("DurationSeconds = %v, want 300.0", route.DurationSeconds)
	}
	if len(route.Coordinates) != 3 {
		t.Fatalf("len(Coordinates) = %d, want 3", len(route.Coordinates))
	}
	if route.Coordinates[0] != [2]float64{-66.9036, 10.4806} {
		t.Errorf("Coordinates[0] = %v, want [-66.9036, 10.4806]", route.Coordinates[0])
	}

	if gotAuth != "test-key" {
		t.Errorf("Authorization header = %q, want %q (no Bearer prefix)", gotAuth, "test-key")
	}
	// Request coordinates must be sent as [lng, lat] — the opposite of the
	// Waypoint{Lat, Lng} struct order.
	if !strings.Contains(gotBody, "[-66.9036,10.4806]") {
		t.Errorf("request body did not send [lng,lat] order; got %s", gotBody)
	}
}

func TestORSClient_GetRoute_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "invalid api key"}`))
	}))
	defer server.Close()

	client := NewORSClient(server.URL, "bad-key")
	route, err := client.GetRoute(context.Background(), []Waypoint{
		{Lat: 10.4806, Lng: -66.9036},
		{Lat: 10.47, Lng: -66.89},
	})
	if err == nil {
		t.Fatal("expected an error for a non-200 ORS response")
	}
	if route != nil {
		t.Errorf("expected nil route on error, got %+v", route)
	}
}

func TestORSClient_GetRoute_MalformedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"features": []}`))
	}))
	defer server.Close()

	client := NewORSClient(server.URL, "test-key")
	_, err := client.GetRoute(context.Background(), []Waypoint{
		{Lat: 10.4806, Lng: -66.9036},
		{Lat: 10.47, Lng: -66.89},
	})
	if err == nil {
		t.Fatal("expected an error when the response has no usable geometry")
	}
}

func TestORSClient_GetRoute_TooFewWaypoints(t *testing.T) {
	client := NewORSClient("", "test-key")
	_, err := client.GetRoute(context.Background(), []Waypoint{{Lat: 10.48, Lng: -66.9}})
	if err == nil {
		t.Fatal("expected an error for fewer than 2 waypoints")
	}
}
