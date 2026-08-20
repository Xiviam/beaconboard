package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPCheckerChecksStatusAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Probe") != "beacon" {
			t.Errorf("X-Probe = %q", request.Header.Get("X-Probe"))
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	checker := NewHTTPChecker("BeaconBoard/test")
	defer checker.Close()
	result := checker.Check(context.Background(), Target{
		ID:             "api",
		URL:            server.URL,
		Method:         http.MethodGet,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusNoContent,
		Headers:        map[string]string{"X-Probe": "beacon"},
	})

	if !result.Healthy || result.StatusCode != http.StatusNoContent || result.Error != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Latency < 0 || result.CheckedAt.IsZero() {
		t.Fatalf("missing timing data: %+v", result)
	}
}

func TestHTTPCheckerReportsUnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	checker := NewHTTPChecker("BeaconBoard/test")
	defer checker.Close()
	result := checker.Check(context.Background(), Target{
		ID:             "api",
		URL:            server.URL,
		Method:         http.MethodGet,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusOK,
	})

	if result.Healthy || result.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Error != "expected HTTP 200, received 503" {
		t.Fatalf("Error = %q", result.Error)
	}
}

func TestHTTPCheckerHonorsTargetTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPChecker("BeaconBoard/test")
	defer checker.Close()
	result := checker.Check(context.Background(), Target{
		ID:             "slow",
		URL:            server.URL,
		Method:         http.MethodGet,
		Timeout:        10 * time.Millisecond,
		ExpectedStatus: http.StatusOK,
	})

	if result.Healthy || result.Error == "" {
		t.Fatalf("expected timeout result, got %+v", result)
	}
}

func TestHTTPCheckerObservesRedirectStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(response, request, "/final", http.StatusFound)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	checker := NewHTTPChecker("BeaconBoard/test")
	defer checker.Close()
	result := checker.Check(context.Background(), Target{
		ID:             "redirect",
		URL:            server.URL + "/redirect",
		Method:         http.MethodGet,
		Timeout:        time.Second,
		ExpectedStatus: http.StatusFound,
	})

	if !result.Healthy || result.StatusCode != http.StatusFound {
		t.Fatalf("unexpected result: %+v", result)
	}
}
