package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Xiviam/beaconboard/internal/config"
	"github.com/Xiviam/beaconboard/internal/monitor"
	"github.com/Xiviam/beaconboard/internal/server"
)

var (
	version = "dev"
	commit  = "none"
	builtAt = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "beaconboard:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) > 0 && arguments[0] == "healthcheck" {
		return healthcheck(arguments[1:])
	}
	flags := flag.NewFlagSet("beaconboard", flag.ContinueOnError)
	configPath := flags.String("config", environment("BEACONBOARD_CONFIG", "beaconboard.json"), "path to JSON configuration")
	listen := flags.String("listen", os.Getenv("BEACONBOARD_LISTEN"), "override listen address")
	showVersion := flags.Bool("version", false, "print build version")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *showVersion {
		fmt.Printf("beaconboard %s (commit %s, built %s)\n", version, commit, builtAt)
		return nil
	}

	settings, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *listen != "" {
		settings.Listen = *listen
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store := monitor.NewStore(settings.Targets, settings.HistoryLimit)
	checker := monitor.NewHTTPChecker("BeaconBoard/" + version)
	defer checker.Close()
	scheduler := monitor.NewScheduler(settings.Targets, checker, store)
	api := server.New(store, logger)

	rootContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(rootContext)
	defer cancel()
	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		scheduler.Run(ctx)
	}()

	logger.Info(
		"BeaconBoard started",
		"listen", settings.Listen,
		"targets", len(settings.Targets),
		"version", version,
	)
	serveErr := api.ListenAndServe(ctx, settings.Listen)
	cancel()
	<-schedulerDone
	return serveErr
}

func healthcheck(arguments []string) error {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	configPath := flags.String("config", environment("BEACONBOARD_CONFIG", "beaconboard.json"), "path to JSON configuration")
	listen := flags.String("listen", os.Getenv("BEACONBOARD_LISTEN"), "override listen address")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	healthURL := os.Getenv("BEACONBOARD_HEALTH_URL")
	if healthURL == "" {
		address := *listen
		if address == "" {
			settings, err := config.Load(*configPath)
			if err != nil {
				return err
			}
			address = settings.Listen
		}
		var err error
		healthURL, err = localHealthURL(address)
		if err != nil {
			return err
		}
	}

	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(healthURL)
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health request returned HTTP %d", response.StatusCode)
	}
	return nil
}

func localHealthURL(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("parse listen address: %w", err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   "/healthz",
	}).String(), nil
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
