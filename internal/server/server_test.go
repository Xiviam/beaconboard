package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Xiviam/beaconboard/internal/monitor"
)

func TestAPIAndMetrics(t *testing.T) {
	store := monitor.NewStore([]monitor.Target{{
		ID:             "api",
		Name:           "Public API",
		URL:            "https://example.com/health",
		Method:         http.MethodGet,
		Interval:       30 * time.Second,
		Timeout:        5 * time.Second,
		ExpectedStatus: http.StatusOK,
	}}, 5)
	store.Record(monitor.Result{
		TargetID:   "api",
		CheckedAt:  time.Unix(10, 0).UTC(),
		Latency:    125 * time.Millisecond,
		StatusCode: http.StatusOK,
		Healthy:    true,
	})
	handler := New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()

	t.Run("health", func(t *testing.T) {
		response := request(t, handler, "/healthz")
		if response.Code != http.StatusOK || response.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("unexpected response: status=%d headers=%v", response.Code, response.Header())
		}
	})

	t.Run("list", func(t *testing.T) {
		response := request(t, handler, "/api/v1/monitors")
		var body struct {
			Monitors []monitor.Monitor `json:"monitors"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(body.Monitors) != 1 || !body.Monitors[0].Healthy || body.Monitors[0].LatencyMS != 125 {
			t.Fatalf("unexpected body: %+v", body)
		}
	})

	t.Run("detail", func(t *testing.T) {
		response := request(t, handler, "/api/v1/monitors/api")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"history"`) {
			t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		response := request(t, handler, "/api/v1/monitors/missing")
		if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "monitor_not_found") {
			t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
		}
	})

	t.Run("metrics", func(t *testing.T) {
		response := request(t, handler, "/metrics")
		body := response.Body.String()
		if !strings.Contains(body, `beaconboard_target_up{target="api"} 1`) ||
			!strings.Contains(body, `beaconboard_checks_total{target="api"} 1`) {
			t.Fatalf("unexpected metrics:\n%s", body)
		}
	})

	t.Run("embedded dashboard", func(t *testing.T) {
		response := request(t, handler, "/")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "BeaconBoard") {
			t.Fatalf("unexpected dashboard: %d %s", response.Code, response.Body.String())
		}
		asset := request(t, handler, "/app.js")
		if asset.Code != http.StatusOK || !strings.Contains(asset.Header().Get("Content-Type"), "javascript") {
			t.Fatalf("unexpected asset: %d %v", asset.Code, asset.Header())
		}
	})
}

func TestEventStreamStartsWithSnapshot(t *testing.T) {
	store := monitor.NewStore([]monitor.Target{{ID: "api", Name: "API"}}, 1)
	handler := New(store, slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: snapshot") {
		t.Fatalf("unexpected stream: %d %s", response.Code, response.Body.String())
	}
}

func request(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
