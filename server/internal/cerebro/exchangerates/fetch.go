// Package exchangerates fetches USD->{targets} reference rates and caches them
// in cerebro_exchange_rates. FIR-40 — the daily refresh runs from
// runs.firtal.com via the cmd/cerebro_fetch_exchange_rates entrypoint; the HTTP
// fetch is deliberately kept out of the request path (display always reads the
// cached row).
package exchangerates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// DefaultEndpoint is the Frankfurter ECB-reference-rate API (free, no key).
const DefaultEndpoint = "https://api.frankfurter.dev/v1/latest"

// Snapshot is one fetch result: the source date and target->rate pairs.
type Snapshot struct {
	// Date is the rate publication date (YYYY-MM-DD) reported by the source.
	Date  string
	Rates map[string]float64
}

// frankfurterResponse mirrors the Frankfurter payload:
//
//	{"amount":1.0,"base":"USD","date":"2026-06-04","rates":{"DKK":6.8,"EUR":0.92}}
type frankfurterResponse struct {
	Base  string             `json:"base"`
	Date  string             `json:"date"`
	Rates map[string]float64 `json:"rates"`
}

// ParseSnapshot decodes a Frankfurter response body. Kept separate from the
// HTTP call so it is unit-testable without a network.
func ParseSnapshot(body []byte) (Snapshot, error) {
	var resp frankfurterResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return Snapshot{}, fmt.Errorf("decode rates response: %w", err)
	}
	if len(resp.Rates) == 0 {
		return Snapshot{}, fmt.Errorf("rates response contained no rates")
	}
	if resp.Date == "" {
		return Snapshot{}, fmt.Errorf("rates response missing date")
	}
	return Snapshot{Date: resp.Date, Rates: resp.Rates}, nil
}

// Fetch retrieves the latest USD->symbols rates from the Frankfurter API.
func Fetch(ctx context.Context, client *http.Client, endpoint, base string, symbols []string) (Snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("build request: %w", err)
	}
	q := req.URL.Query()
	q.Set("base", base)
	if len(symbols) > 0 {
		q.Set("symbols", joinComma(symbols))
	}
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("fetch rates: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("rates endpoint returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read rates body: %w", err)
	}
	return ParseSnapshot(body)
}

// Store upserts every rate in the snapshot into cerebro_exchange_rates. Returns
// the number of rows written.
func Store(ctx context.Context, q *cerebrodb.Queries, base string, snap Snapshot) (int, error) {
	fetchedAt, err := time.Parse("2006-01-02", snap.Date)
	if err != nil {
		return 0, fmt.Errorf("parse snapshot date %q: %w", snap.Date, err)
	}
	written := 0
	for target, rate := range snap.Rates {
		num, err := toNumeric(rate)
		if err != nil {
			return written, fmt.Errorf("convert rate for %s: %w", target, err)
		}
		if err := q.UpsertCerebroExchangeRate(ctx, cerebrodb.UpsertCerebroExchangeRateParams{
			BaseCurrency:   base,
			TargetCurrency: target,
			Rate:           num,
			FetchedAt:      pgtype.Timestamptz{Time: fetchedAt.UTC(), Valid: true},
		}); err != nil {
			return written, fmt.Errorf("upsert rate for %s: %w", target, err)
		}
		written++
	}
	return written, nil
}

// toNumeric converts a float rate into a pgtype.Numeric without float drift by
// formatting to a fixed 8-decimal string and letting pgtype parse it.
func toNumeric(rate float64) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if err := n.Scan(strconv.FormatFloat(rate, 'f', 8, 64)); err != nil {
		return pgtype.Numeric{}, err
	}
	return n, nil
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
