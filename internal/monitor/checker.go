package monitor

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const responseDrainLimit = 32 << 10

// Checker performs one target check.
type Checker interface {
	Check(context.Context, Target) Result
}

// HTTPChecker uses a shared, connection-pooling HTTP client.
type HTTPChecker struct {
	client    *http.Client
	userAgent string
}

// NewHTTPChecker creates a checker with conservative transport timeouts.
func NewHTTPChecker(userAgent string) *HTTPChecker {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = 10 * time.Second

	return &HTTPChecker{
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		userAgent: userAgent,
	}
}

// Check executes one request and compares its status with the target policy.
func (c *HTTPChecker) Check(parent context.Context, target Target) Result {
	startedAt := time.Now()
	result := Result{TargetID: target.ID}

	ctx, cancel := context.WithTimeout(parent, target.Timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, target.Method, target.URL, nil)
	if err != nil {
		result.CheckedAt = time.Now().UTC()
		result.Latency = time.Since(startedAt)
		result.Error = err.Error()
		return result
	}
	for key, value := range target.Headers {
		request.Header.Set(key, value)
	}
	if request.Header.Get("User-Agent") == "" && c.userAgent != "" {
		request.Header.Set("User-Agent", c.userAgent)
	}

	response, err := c.client.Do(request)
	result.CheckedAt = time.Now().UTC()
	result.Latency = time.Since(startedAt)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseDrainLimit))

	result.StatusCode = response.StatusCode
	result.Healthy = response.StatusCode == target.ExpectedStatus
	if !result.Healthy {
		result.Error = fmt.Sprintf(
			"expected HTTP %d, received %d",
			target.ExpectedStatus,
			response.StatusCode,
		)
	}
	return result
}

// Close releases idle connections held by the checker.
func (c *HTTPChecker) Close() {
	c.client.CloseIdleConnections()
}
