package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeConfig(t, `{
		"targets": [{"id":"api","url":"https://example.com/health"}]
	}`)

	settings, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if settings.Listen != ":8080" || settings.HistoryLimit != 120 {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
	target := settings.Targets[0]
	if target.Name != "api" || target.Method != "GET" {
		t.Fatalf("unexpected target defaults: %+v", target)
	}
	if target.Interval != 30*time.Second || target.Timeout != 5*time.Second {
		t.Fatalf("unexpected duration defaults: %+v", target)
	}
	if target.ExpectedStatus != 200 {
		t.Fatalf("ExpectedStatus = %d, want 200", target.ExpectedStatus)
	}
}

func TestLoadRejectsInvalidConfigurations(t *testing.T) {
	tests := map[string]struct {
		config string
		want   string
	}{
		"unknown field": {
			config: `{"unknown":true,"targets":[{"id":"api","url":"https://example.com"}]}`,
			want:   "unknown field",
		},
		"duplicate id": {
			config: `{"targets":[{"id":"api","url":"https://one.example"},{"id":"api","url":"https://two.example"}]}`,
			want:   "duplicate id",
		},
		"relative url": {
			config: `{"targets":[{"id":"api","url":"/health"}]}`,
			want:   "absolute http or https",
		},
		"url credentials": {
			config: `{"targets":[{"id":"api","url":"https://user:secret@example.com"}]}`,
			want:   "must not contain user credentials",
		},
		"short interval": {
			config: `{"targets":[{"id":"api","url":"https://example.com","interval":"100ms"}]}`,
			want:   "at least 1s",
		},
		"timeout exceeds interval": {
			config: `{"targets":[{"id":"api","url":"https://example.com","interval":"1s","timeout":"2s"}]}`,
			want:   "must not exceed interval",
		},
		"managed header": {
			config: `{"targets":[{"id":"api","url":"https://example.com","headers":{"Host":"elsewhere.example"}}]}`,
			want:   "managed by BeaconBoard",
		},
		"invalid header name": {
			config: `{"targets":[{"id":"api","url":"https://example.com","headers":{"Bad Header":"value"}}]}`,
			want:   "invalid header name",
		},
		"header newline": {
			config: `{"targets":[{"id":"api","url":"https://example.com","headers":{"X-Probe":"yes\r\nInjected: true"}}]}`,
			want:   "contains a newline",
		},
		"multiple values": {
			config: `{"targets":[{"id":"api","url":"https://example.com"}]} {}`,
			want:   "multiple JSON values",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, test.config))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "beaconboard.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
