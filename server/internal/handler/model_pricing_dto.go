package handler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/internal/pricing"
)

// HTTP prices use the API's snake_case contract. The catalog document keeps
// its original field names so the Go and TypeScript offline bundles agree.
type modelPricingRow struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Provider   string  `json:"provider,omitempty"`
	Model      string  `json:"model,omitempty"`
	Source     string  `json:"source,omitempty"`
	SourceURL  string  `json:"source_url,omitempty"`
}

func (r *modelPricingRow) UnmarshalJSON(data []byte) error {
	type plain modelPricingRow
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range []string{"input", "output", "cache_read", "cache_write"} {
		v, ok := fields[key]
		if !ok || string(v) == "null" {
			return fmt.Errorf("missing price: %s", key)
		}
	}
	*r = modelPricingRow(value)
	return r.price().Validate()
}

func (r modelPricingRow) price() pricing.Row {
	return pricing.Row{
		Input: r.Input, Output: r.Output, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite,
		Provider: r.Provider, Model: r.Model, Source: r.Source, SourceURL: r.SourceURL,
	}
}

func modelPricingRows(rows map[string]pricing.Row) map[string]modelPricingRow {
	result := make(map[string]modelPricingRow, len(rows))
	for key, row := range rows {
		result[key] = modelPricingRow{
			Input: row.Input, Output: row.Output, CacheRead: row.CacheRead, CacheWrite: row.CacheWrite,
			Provider: row.Provider, Model: row.Model, Source: row.Source, SourceURL: row.SourceURL,
		}
	}
	return result
}

type modelPricingSnapshot struct {
	Version     string                     `json:"version"`
	Rows        map[string]modelPricingRow `json:"rows"`
	Aliases     map[string]string          `json:"aliases"`
	Overrides   map[string]modelPricingRow `json:"overrides"`
	Revision    int64                      `json:"revision"`
	CanManage   bool                       `json:"can_manage"`
	CheckedAt   *time.Time                 `json:"checked_at"`
	SucceededAt *time.Time                 `json:"succeeded_at"`
	LastError   string                     `json:"last_error"`
	Timezone    string                     `json:"timezone"`
}

func modelPricingResponse(snapshot pricing.Snapshot) modelPricingSnapshot {
	return modelPricingSnapshot{
		Version: snapshot.Version, Rows: modelPricingRows(snapshot.Rows), Aliases: snapshot.Aliases,
		Overrides: modelPricingRows(snapshot.Overrides), Revision: snapshot.Revision,
		CanManage: snapshot.CanManage, CheckedAt: snapshot.CheckedAt, SucceededAt: snapshot.SucceededAt,
		LastError: snapshot.LastError, Timezone: snapshot.Timezone,
	}
}
