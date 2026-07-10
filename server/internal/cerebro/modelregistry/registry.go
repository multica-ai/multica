// Package modelregistry implements the single-source model registry
// (FIR-2698): one versioned document holding every LLM model's display label,
// provider, context window, and list prices, governed by the shared
// propose → review → approve pattern (server/internal/cerebro/versioning).
//
// It replaces four hand-maintained in-code tables (pkg/pricing, internal/
// metrics, sessions/context_window.go, and the frontend's MODEL_PRICING) that
// drifted apart because every price change had to be edited in five places —
// the FIR-2689 root cause. The registry row is a deployment-wide singleton;
// consumers read through the in-process store (store.go), which is loaded at
// startup and refreshed on every merge/rollback.
package modelregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/versioning"
)

// TxStarter is the subset of *pgxpool.Pool the service needs to open a
// transaction. Matches agentoffice.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service wires the model-registry governance queries together.
type Service struct {
	Cerebro *cerebrodb.Queries
	Tx      TxStarter
}

// New constructs the service.
func New(cerebro *cerebrodb.Queries, tx TxStarter) *Service {
	return &Service{Cerebro: cerebro, Tx: tx}
}

// ModelEntry is one model's metadata. Prices are USD per million tokens (the
// human-readable unit; the pricing shim converts to cents). ContextWindow 0
// means "not curated" — consumers apply their conservative default.
type ModelEntry struct {
	Label                string  `json:"label"`
	Provider             string  `json:"provider"`
	ContextWindow        int64   `json:"context_window"`
	InputUSDPerMtok      float64 `json:"input_usd_per_mtok"`
	OutputUSDPerMtok     float64 `json:"output_usd_per_mtok"`
	CacheReadUSDPerMtok  float64 `json:"cache_read_usd_per_mtok"`
	CacheWriteUSDPerMtok float64 `json:"cache_write_usd_per_mtok"`
}

// Snapshot is the whole registry document stored in model_registry.snapshot,
// model_registry_version.snapshot and
// model_registry_change_request.proposed_snapshot.
type Snapshot struct {
	FallbackModel string                `json:"fallback_model"`
	Models        map[string]ModelEntry `json:"models"`
}

// EncodeSnapshot marshals a snapshot to JSONB bytes, never returning nil so
// the NOT NULL column always gets a value.
func EncodeSnapshot(s Snapshot) []byte {
	b, err := json.Marshal(s)
	if err != nil || len(b) == 0 {
		return []byte("{}")
	}
	return b
}

// DecodeSnapshot parses stored JSONB back into a snapshot.
func DecodeSnapshot(raw []byte) Snapshot {
	var s Snapshot
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	if s.Models == nil {
		s.Models = map[string]ModelEntry{}
	}
	return s
}

// ValidateSnapshot rejects documents that would corrupt cost computation:
// blank model ids, negative prices or windows, or a fallback model that is
// not in the table.
func ValidateSnapshot(s Snapshot) error {
	if len(s.Models) == 0 {
		return fmt.Errorf("snapshot must contain at least one model")
	}
	for id, e := range s.Models {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("model id must not be blank")
		}
		if id != strings.ToLower(strings.TrimSpace(id)) {
			return fmt.Errorf("model id %q must be lowercase with no surrounding whitespace", id)
		}
		if e.ContextWindow < 0 {
			return fmt.Errorf("model %q: context_window must not be negative", id)
		}
		if e.InputUSDPerMtok < 0 || e.OutputUSDPerMtok < 0 ||
			e.CacheReadUSDPerMtok < 0 || e.CacheWriteUSDPerMtok < 0 {
			return fmt.Errorf("model %q: prices must not be negative", id)
		}
	}
	if s.FallbackModel == "" {
		return fmt.Errorf("fallback_model is required")
	}
	if _, ok := s.Models[s.FallbackModel]; !ok {
		return fmt.Errorf("fallback_model %q is not in the model table", s.FallbackModel)
	}
	return nil
}

// RenderSnapshot produces a stable, human-readable text rendering for diffing
// and review: fallback first, then one line per model in sorted id order.
func RenderSnapshot(s Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "fallback_model: %s\n", s.FallbackModel)
	ids := make([]string, 0, len(s.Models))
	for id := range s.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := s.Models[id]
		fmt.Fprintf(&b, "%s: label=%q provider=%s context_window=%d input=%s output=%s cache_read=%s cache_write=%s\n",
			id, e.Label, e.Provider, e.ContextWindow,
			fmtUSD(e.InputUSDPerMtok), fmtUSD(e.OutputUSDPerMtok),
			fmtUSD(e.CacheReadUSDPerMtok), fmtUSD(e.CacheWriteUSDPerMtok))
	}
	return b.String()
}

func fmtUSD(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// DiffSnapshots renders each snapshot and returns a unified-diff string
// between them via the shared diff engine.
func DiffSnapshots(base, proposed Snapshot) string {
	return versioning.UnifiedDiff(RenderSnapshot(base), RenderSnapshot(proposed), "model-registry")
}

// ApplySnapshotTx writes a snapshot onto the live registry row and bumps
// current_version, on the supplied transaction-scoped queries. The caller
// owns the transaction.
func (s *Service) ApplySnapshotTx(ctx context.Context, qtx *cerebrodb.Queries, snap Snapshot, newVersion string) (cerebrodb.ModelRegistry, error) {
	reg, err := qtx.ApplyModelRegistrySnapshot(ctx, cerebrodb.ApplyModelRegistrySnapshotParams{
		Snapshot:       EncodeSnapshot(snap),
		CurrentVersion: newVersion,
	})
	if err != nil {
		return cerebrodb.ModelRegistry{}, fmt.Errorf("apply snapshot: %w", err)
	}
	return reg, nil
}
