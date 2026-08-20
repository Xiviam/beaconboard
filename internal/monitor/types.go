package monitor

import "time"

// Target describes an HTTP endpoint and its check policy.
type Target struct {
	ID             string
	Name           string
	URL            string
	Method         string
	Interval       time.Duration
	Timeout        time.Duration
	ExpectedStatus int
	Headers        map[string]string
}

// Result is the outcome of one endpoint check.
type Result struct {
	TargetID   string
	CheckedAt  time.Time
	Latency    time.Duration
	StatusCode int
	Healthy    bool
	Error      string
}

// Check is the JSON-friendly representation of a historical result.
type Check struct {
	Healthy    bool      `json:"healthy"`
	StatusCode int       `json:"status_code"`
	LatencyMS  float64   `json:"latency_ms"`
	CheckedAt  time.Time `json:"checked_at"`
	Error      string    `json:"error,omitempty"`
}

// Monitor is the current public state of a target.
type Monitor struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	URL        string     `json:"url"`
	Healthy    bool       `json:"healthy"`
	Pending    bool       `json:"pending"`
	StatusCode int        `json:"status_code"`
	LatencyMS  float64    `json:"latency_ms"`
	CheckedAt  *time.Time `json:"checked_at"`
	Error      string     `json:"error,omitempty"`
	Checks     uint64     `json:"checks"`
	Failures   uint64     `json:"failures"`
}

// Detail includes a monitor's check policy and rolling history.
type Detail struct {
	Monitor
	Method         string        `json:"method"`
	Interval       time.Duration `json:"-"`
	Timeout        time.Duration `json:"-"`
	IntervalText   string        `json:"interval"`
	TimeoutText    string        `json:"timeout"`
	ExpectedStatus int           `json:"expected_status"`
	History        []Check       `json:"history"`
}
