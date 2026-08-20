package server

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Xiviam/beaconboard/internal/monitor"
)

//go:embed web/*
var webAssets embed.FS

// Server exposes the dashboard, API, event stream, and metrics.
type Server struct {
	store   *monitor.Store
	logger  *slog.Logger
	handler http.Handler
}

// New builds a fully wired HTTP server.
func New(store *monitor.Store, logger *slog.Logger) *Server {
	server := &Server{store: store, logger: logger}
	server.handler = server.routes()
	return server
}

// Handler exposes the server routes for tests and custom hosting.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ListenAndServe runs until the context is cancelled or the listener fails.
func (s *Server) ListenAndServe(ctx context.Context, address string) error {
	httpServer := &http.Server{
		Addr:              address,
		Handler:           s.handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		// Streaming responses require no global WriteTimeout.
		WriteTimeout: 0,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("graceful HTTP shutdown failed", "error", err)
			_ = httpServer.Close()
		}
	}()

	err := httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	return err
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/monitors", s.listMonitors)
	mux.HandleFunc("GET /api/v1/monitors/{id}", s.getMonitor)
	mux.HandleFunc("GET /api/v1/events", s.events)
	mux.HandleFunc("GET /metrics", s.metrics)

	webRoot, err := fs.Sub(webAssets, "web")
	if err != nil {
		panic(fmt.Sprintf("load embedded dashboard: %v", err))
	}
	mux.Handle("GET /", http.FileServer(http.FS(webRoot)))
	return securityHeaders(mux)
}

func (s *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listMonitors(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{"monitors": s.store.List()})
}

func (s *Server) getMonitor(response http.ResponseWriter, request *http.Request) {
	detail, ok := s.store.Get(request.PathValue("id"))
	if !ok {
		writeError(response, http.StatusNotFound, "monitor_not_found", "monitor does not exist")
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (s *Server) events(response http.ResponseWriter, request *http.Request) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "stream_unsupported", "streaming is unavailable")
		return
	}
	updates, unsubscribe := s.store.Subscribe()
	defer unsubscribe()

	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	if !writeSSE(response, "snapshot", map[string]any{"monitors": s.store.List()}) {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case update, open := <-updates:
			if !open {
				return
			}
			if !writeSSE(response, "check", update) {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(response, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-request.Context().Done():
			return
		}
	}
}

func (s *Server) metrics(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	monitors := s.store.List()
	fmt.Fprintln(response, "# HELP beaconboard_target_known Whether a target has completed its first check.")
	fmt.Fprintln(response, "# TYPE beaconboard_target_known gauge")
	for _, item := range monitors {
		known := boolMetric(!item.Pending)
		fmt.Fprintf(response, "beaconboard_target_known{target=%q} %d\n", metricLabel(item.ID), known)
	}
	fmt.Fprintln(response, "# HELP beaconboard_target_up Whether the latest target check succeeded.")
	fmt.Fprintln(response, "# TYPE beaconboard_target_up gauge")
	for _, item := range monitors {
		fmt.Fprintf(response, "beaconboard_target_up{target=%q} %d\n", metricLabel(item.ID), boolMetric(item.Healthy))
	}
	fmt.Fprintln(response, "# HELP beaconboard_checks_total Total checks performed for a target.")
	fmt.Fprintln(response, "# TYPE beaconboard_checks_total counter")
	fmt.Fprintln(response, "# HELP beaconboard_check_failures_total Total failed checks for a target.")
	fmt.Fprintln(response, "# TYPE beaconboard_check_failures_total counter")
	fmt.Fprintln(response, "# HELP beaconboard_last_check_duration_seconds Duration of the latest target check.")
	fmt.Fprintln(response, "# TYPE beaconboard_last_check_duration_seconds gauge")
	for _, item := range monitors {
		label := metricLabel(item.ID)
		fmt.Fprintf(response, "beaconboard_checks_total{target=%q} %d\n", label, item.Checks)
		fmt.Fprintf(response, "beaconboard_check_failures_total{target=%q} %d\n", label, item.Failures)
		fmt.Fprintf(response, "beaconboard_last_check_duration_seconds{target=%q} %s\n", label, strconv.FormatFloat(item.LatencyMS/1000, 'f', 6, 64))
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(response, request)
	})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writeSSE(response http.ResponseWriter, event string, value any) bool {
	payload, err := json.Marshal(value)
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event, payload)
	return err == nil
}

func metricLabel(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func boolMetric(value bool) int {
	if value {
		return 1
	}
	return 0
}
