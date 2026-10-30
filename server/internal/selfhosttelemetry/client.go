package selfhosttelemetry

import (
	"bytes"
	"context"
	"net/http"
	"time"
)

const (
	productionEndpoint = "https://telemetry.multica.ai/v1/telemetry/events"
	httpTimeout        = 5 * time.Second
)

type deliveryDisposition int

const (
	deliveryRetry deliveryDisposition = iota
	deliverySuccess
	deliveryStopForDay
)

type eventSender interface {
	Send(context.Context, []byte) deliveryDisposition
	Close()
}

// HTTPClient only knows the fixed first-party V1 endpoint. The unexported test
// constructor below permits local fake servers without creating a deployment
// configuration surface that could redirect production telemetry.
type HTTPClient struct {
	endpoint string
	client   *http.Client
}

func newHTTPClient() *HTTPClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &HTTPClient{
		endpoint: productionEndpoint,
		client: &http.Client{
			Transport: transport,
			Timeout:   httpTimeout,
			// Never forward the snapshot to a redirect target. Endpoint ownership
			// is part of the privacy boundary, not merely a convenience default.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func newHTTPClientForTest(endpoint string, client *http.Client) *HTTPClient {
	return &HTTPClient{endpoint: endpoint, client: client}
}

func (c *HTTPClient) Send(ctx context.Context, body []byte) deliveryDisposition {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return deliveryStopForDay
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return deliveryRetry
	}
	// The response body is deliberately never read or logged.
	_ = resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusAccepted:
		return deliverySuccess
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return deliveryRetry
	default:
		return deliveryStopForDay
	}
}

func (c *HTTPClient) Close() {
	if c == nil || c.client == nil {
		return
	}
	c.client.CloseIdleConnections()
}
