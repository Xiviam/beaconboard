package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Xiviam/beaconboard/internal/monitor"
)

const (
	defaultListen       = ":8080"
	defaultHistoryLimit = 120
	defaultInterval     = 30 * time.Second
	defaultTimeout      = 5 * time.Second
	defaultStatus       = 200
	maxConfigSize       = 1 << 20
	maxTargets          = 256
)

var targetIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// Runtime is a validated application configuration.
type Runtime struct {
	Listen       string
	HistoryLimit int
	Targets      []monitor.Target
}

type fileConfig struct {
	Listen       string       `json:"listen"`
	HistoryLimit int          `json:"history_limit"`
	Targets      []fileTarget `json:"targets"`
}

type fileTarget struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Interval       string            `json:"interval"`
	Timeout        string            `json:"timeout"`
	ExpectedStatus int               `json:"expected_status"`
	Headers        map[string]string `json:"headers"`
}

// Load reads, strictly decodes, defaults, and validates a JSON config file.
func Load(path string) (Runtime, error) {
	file, err := os.Open(path)
	if err != nil {
		return Runtime{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Runtime{}, fmt.Errorf("stat config: %w", err)
	}
	if info.Size() > maxConfigSize {
		return Runtime{}, fmt.Errorf("config exceeds %d bytes", maxConfigSize)
	}

	var raw fileConfig
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Runtime{}, fmt.Errorf("decode config: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Runtime{}, err
	}
	return validate(raw)
}

func validate(raw fileConfig) (Runtime, error) {
	runtime := Runtime{Listen: raw.Listen, HistoryLimit: raw.HistoryLimit}
	if runtime.Listen == "" {
		runtime.Listen = defaultListen
	}
	if runtime.HistoryLimit == 0 {
		runtime.HistoryLimit = defaultHistoryLimit
	}
	if runtime.HistoryLimit < 1 || runtime.HistoryLimit > 10_000 {
		return Runtime{}, errors.New("history_limit must be between 1 and 10000")
	}
	if len(raw.Targets) == 0 {
		return Runtime{}, errors.New("at least one target is required")
	}
	if len(raw.Targets) > maxTargets {
		return Runtime{}, fmt.Errorf("at most %d targets are allowed", maxTargets)
	}

	seen := make(map[string]struct{}, len(raw.Targets))
	for index, item := range raw.Targets {
		target, err := validateTarget(item)
		if err != nil {
			return Runtime{}, fmt.Errorf("target %d: %w", index+1, err)
		}
		if _, exists := seen[target.ID]; exists {
			return Runtime{}, fmt.Errorf("target %d: duplicate id %q", index+1, target.ID)
		}
		seen[target.ID] = struct{}{}
		runtime.Targets = append(runtime.Targets, target)
	}
	return runtime, nil
}

func validateTarget(raw fileTarget) (monitor.Target, error) {
	if !targetIDPattern.MatchString(raw.ID) {
		return monitor.Target{}, errors.New("id must match [a-zA-Z0-9][a-zA-Z0-9_-]{0,63}")
	}
	parsedURL, err := url.Parse(raw.URL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return monitor.Target{}, errors.New("url must be an absolute http or https URL")
	}
	if parsedURL.User != nil {
		return monitor.Target{}, errors.New("url must not contain user credentials")
	}
	if parsedURL.Fragment != "" {
		return monitor.Target{}, errors.New("url must not contain a fragment")
	}

	name := strings.TrimSpace(raw.Name)
	if name == "" {
		name = raw.ID
	}
	method := strings.ToUpper(strings.TrimSpace(raw.Method))
	if method == "" {
		method = "GET"
	}
	if method != "GET" && method != "HEAD" {
		return monitor.Target{}, errors.New("method must be GET or HEAD")
	}

	interval, err := parseDuration(raw.Interval, defaultInterval)
	if err != nil || interval < time.Second {
		return monitor.Target{}, errors.New("interval must be a duration of at least 1s")
	}
	timeout, err := parseDuration(raw.Timeout, defaultTimeout)
	if err != nil || timeout <= 0 {
		return monitor.Target{}, errors.New("timeout must be a positive duration")
	}
	if timeout > interval {
		return monitor.Target{}, errors.New("timeout must not exceed interval")
	}
	status := raw.ExpectedStatus
	if status == 0 {
		status = defaultStatus
	}
	if status < 100 || status > 599 {
		return monitor.Target{}, errors.New("expected_status must be between 100 and 599")
	}

	headers, err := validateHeaders(raw.Headers)
	if err != nil {
		return monitor.Target{}, err
	}

	return monitor.Target{
		ID:             raw.ID,
		Name:           name,
		URL:            parsedURL.String(),
		Method:         method,
		Interval:       interval,
		Timeout:        timeout,
		ExpectedStatus: status,
		Headers:        headers,
	}, nil
}

func parseDuration(value string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return time.ParseDuration(value)
}

func validateHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	copy := make(map[string]string, len(headers))
	for key, value := range headers {
		if !validHeaderName(key) {
			return nil, fmt.Errorf("invalid header name %q", key)
		}
		canonical := textproto.CanonicalMIMEHeaderKey(key)
		if _, forbidden := forbiddenHeaders[canonical]; forbidden {
			return nil, fmt.Errorf("header %q is managed by BeaconBoard", canonical)
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("header %q contains a newline", canonical)
		}
		copy[http.CanonicalHeaderKey(canonical)] = value
	}
	return copy, nil
}

func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	const symbols = "!#$%&'*+-.^_`|~"
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune(symbols, rune(character)) {
			continue
		}
		return false
	}
	return true
}

var forbiddenHeaders = map[string]struct{}{
	"Connection":        {},
	"Content-Length":    {},
	"Host":              {},
	"Keep-Alive":        {},
	"Proxy-Connection":  {},
	"Te":                {},
	"Trailer":           {},
	"Transfer-Encoding": {},
	"Upgrade":           {},
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode config: multiple JSON values are not allowed")
		}
		return fmt.Errorf("decode config: %w", err)
	}
	return nil
}
