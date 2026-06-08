// CEREBRO: FIR-43 client side of the exchange-rate ingestion path. The
// multica-hatchet-worker calls Submit to push a fetched snapshot to the
// backend's POST /api/cerebro/exchange-rates over the internal Sliplane network
// — so the worker holds only a service key, never a database credential.
package exchangerates

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Submit POSTs a snapshot to the backend ingestion endpoint and returns the
// number of rows the backend reported writing. token is presented as a Bearer
// credential; endpoint is the internal URL of the multica-backend service.
func Submit(ctx context.Context, client *http.Client, endpoint, token, base string, snap Snapshot) (int, error) {
	body, err := json.Marshal(IngestRequest{Base: base, Date: snap.Date, Rates: snap.Rates})
	if err != nil {
		return 0, fmt.Errorf("marshal ingest payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build ingest request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post ingest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("ingest endpoint returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}

	var out ingestResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return 0, fmt.Errorf("decode ingest response: %w", err)
	}
	return out.Written, nil
}
