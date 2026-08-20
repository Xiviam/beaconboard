package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("BEACONBOARD_HEALTH_URL", server.URL)

	if err := healthcheck(nil); err != nil {
		t.Fatalf("healthcheck() error = %v", err)
	}
}

func TestHealthcheckRejectsUnhealthyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("BEACONBOARD_HEALTH_URL", server.URL)

	if err := healthcheck(nil); err == nil {
		t.Fatal("healthcheck() error = nil, want failure")
	}
}

func TestLocalHealthURL(t *testing.T) {
	tests := map[string]string{
		":8080":          "http://127.0.0.1:8080/healthz",
		"0.0.0.0:9090":   "http://127.0.0.1:9090/healthz",
		"[::]:7070":      "http://127.0.0.1:7070/healthz",
		"localhost:6060": "http://localhost:6060/healthz",
	}
	for address, expected := range tests {
		actual, err := localHealthURL(address)
		if err != nil {
			t.Fatalf("localHealthURL(%q) error = %v", address, err)
		}
		if actual != expected {
			t.Fatalf("localHealthURL(%q) = %q, want %q", address, actual, expected)
		}
	}
}

func TestEnvironment(t *testing.T) {
	t.Setenv("BEACONBOARD_TEST_VALUE", "configured")
	if value := environment("BEACONBOARD_TEST_VALUE", "fallback"); value != "configured" {
		t.Fatalf("environment() = %q", value)
	}
	if value := environment("BEACONBOARD_MISSING_VALUE", "fallback"); value != "fallback" {
		t.Fatalf("environment() fallback = %q", value)
	}
}
