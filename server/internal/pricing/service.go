package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const DefaultLiteURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
const DefaultModelsURL = "https://models.dev/api.json"

type TxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}
type Service struct {
	queries   *db.Queries
	txs       TxStarter
	Client    *http.Client
	LiteURL   string
	ModelsURL string
	Location  *time.Location
	OnChange  func(workspaceID string)
}

func New(q *db.Queries, txs TxStarter) *Service {
	return &Service{queries: q, txs: txs, Client: &http.Client{Timeout: 30 * time.Second}, LiteURL: DefaultLiteURL, ModelsURL: DefaultModelsURL, Location: time.UTC}
}

func (s *Service) ConfigureFromEnvironment() error {
	tz := strings.TrimSpace(os.Getenv("MODEL_PRICING_TIMEZONE"))
	if tz == "" {
		tz = strings.TrimSpace(os.Getenv("TZ"))
	}
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("invalid model pricing timezone: %w", err)
	}
	s.Location = loc
	if value := os.Getenv("MODEL_PRICING_LITELLM_URL"); value != "" {
		s.LiteURL = value
	}
	if value := os.Getenv("MODEL_PRICING_MODELS_URL"); value != "" {
		s.ModelsURL = value
	}
	return nil
}

type Snapshot struct {
	Catalog
	Overrides   map[string]Row `json:"overrides"`
	Revision    int64          `json:"revision"`
	CanManage   bool           `json:"can_manage"`
	CheckedAt   *time.Time     `json:"checked_at"`
	SucceededAt *time.Time     `json:"succeeded_at"`
	LastError   string         `json:"last_error"`
	Timezone    string         `json:"timezone"`
}

func (s *Service) Snapshot(ctx context.Context, workspaceID pgtype.UUID) (Snapshot, error) {
	result := Snapshot{Catalog: Bundled(), Overrides: map[string]Row{}, Timezone: s.Location.String()}
	state, err := s.queries.GetModelPricingCatalog(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if err == nil {
		if len(state.Catalog) > 0 && string(state.Catalog) != "null" {
			if err := json.Unmarshal(state.Catalog, &result.Catalog); err != nil {
				return result, err
			}
		}
		if state.CheckedAt.Valid {
			value := state.CheckedAt.Time
			result.CheckedAt = &value
		}
		if state.SucceededAt.Valid {
			value := state.SucceededAt.Time
			result.SucceededAt = &value
		}
		result.LastError = state.LastError
	}
	overrides, err := s.queries.GetWorkspaceModelPricing(ctx, workspaceID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if err == nil {
		if err := json.Unmarshal(overrides.Overrides, &result.Overrides); err != nil {
			return result, err
		}
		result.Revision = overrides.Revision
	}
	return result, nil
}

var ErrConflict = errors.New("model prices changed; reload before saving")
var ErrInvalid = errors.New("invalid model prices")
var ErrRefresh = errors.New("price refresh failed")

func (s *Service) SaveOverrides(ctx context.Context, workspaceID, userID pgtype.UUID, revision int64, rows map[string]Row) error {
	if len(rows) > 1000 || revision < 0 {
		return ErrInvalid
	}
	clean := make(map[string]Row, len(rows))
	for key, row := range rows {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" || len(normalized) > 512 || strings.ContainsAny(normalized, "\r\n\t") {
			return ErrInvalid
		}
		if _, duplicate := clean[normalized]; duplicate {
			return ErrInvalid
		}
		if err := row.Validate(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		clean[normalized] = Row{Input: row.Input, Output: row.Output, CacheRead: row.CacheRead, CacheWrite: row.CacheWrite}
	}
	payload, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	tx, err := s.txs.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	// This lock serializes revisions and fences workspace teardown. No FK or
	// dependency row can survive a concurrent workspace deletion.
	if _, err = q.LockWorkspaceForModelPricing(ctx, workspaceID); err != nil {
		return err
	}
	current, err := q.GetWorkspaceModelPricing(ctx, workspaceID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if current.Revision != revision {
		return ErrConflict
	}
	_, err = q.SaveWorkspaceModelPricing(ctx, db.SaveWorkspaceModelPricingParams{WorkspaceID: workspaceID, Overrides: payload, Revision: revision + 1, UpdatedBy: userID})
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if s.OnChange != nil {
		s.OnChange(workspaceID.String())
	}
	return nil
}

type feedState struct {
	ETag string          `json:"etag"`
	Body json.RawMessage `json:"body"`
}
type syncState struct {
	Catalog Catalog   `json:"catalog"`
	Lite    feedState `json:"lite"`
	Models  feedState `json:"models"`
}

func fetchFeed(ctx context.Context, client *http.Client, feedURL string, previous feedState) (feedState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return feedState{}, fmt.Errorf("invalid feed URL")
	}
	req.Header.Set("Accept", "application/json")
	if previous.ETag != "" && len(previous.Body) > 0 {
		req.Header.Set("If-None-Match", previous.ETag)
	}
	res, err := client.Do(req)
	if err != nil {
		// Keep the transport cause without exposing credentials or query strings
		// from a configured feed URL in the workspace-visible diagnostic.
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			err = requestErr.Err
		}
		return feedState{}, fmt.Errorf("price feed request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified && len(previous.Body) > 0 {
		return previous, nil
	}
	if res.StatusCode != http.StatusOK {
		return feedState{}, fmt.Errorf("price feed HTTP %d", res.StatusCode)
	}
	const maxBytes = 32 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return feedState{}, fmt.Errorf("price feed read failed: %w", err)
	}
	if len(body) > maxBytes || !json.Valid(body) {
		return feedState{}, fmt.Errorf("invalid price feed JSON")
	}
	return feedState{ETag: res.Header.Get("ETag"), Body: body}, nil
}

func (s *Service) refreshDue(previous db.ModelPricingCatalog, now time.Time, force bool) (bool, error) {
	if previous.CheckedAt.Valid && now.Sub(previous.CheckedAt.Time) < time.Minute {
		if previous.LastError != "" {
			// A skipped retry must not finalize its scheduler plan as successful.
			return false, fmt.Errorf("%w: refresh cooldown has not elapsed", ErrRefresh)
		}
		return false, nil
	}
	// Only a successful check satisfies the daily schedule. A failure after
	// today's earlier success still needs a retry to clear its diagnostic.
	if !force && previous.LastError == "" && previous.SucceededAt.Valid && !previous.SucceededAt.Time.Before(Midnight(now, s.Location)) {
		return false, nil
	}
	return true, nil
}

// Refresh commits only a fully parsed pair of feeds. A failed refresh updates
// diagnostics but leaves the last usable document intact across restarts.
func (s *Service) Refresh(ctx context.Context, force bool) error {
	tx, err := s.txs.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	locked, err := q.TryLockModelPricingSync(ctx)
	if err != nil {
		return err
	}
	if !locked {
		return nil
	}
	previous, err := q.GetModelPricingSyncState(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if due, err := s.refreshDue(previous, time.Now(), force); !due {
		return err
	}
	var state syncState
	if len(previous.Document) > 0 {
		if err = json.Unmarshal(previous.Document, &state); err != nil {
			return err
		}
	}
	state.Lite, err = fetchFeed(ctx, s.Client, s.LiteURL, state.Lite)
	if err != nil {
		err = fmt.Errorf("LiteLLM: %w", err)
	} else {
		state.Models, err = fetchFeed(ctx, s.Client, s.ModelsURL, state.Models)
		if err != nil {
			err = fmt.Errorf("models.dev: %w", err)
		}
	}
	if err == nil {
		state.Catalog, err = BuildCatalog(state.Lite.Body, state.Models.Body)
	}
	refreshErr := err
	if refreshErr != nil {
		if err = q.RecordModelPricingFailure(ctx, refreshErr.Error()); err != nil {
			return err
		}
	} else {
		payload, marshalErr := json.Marshal(state)
		if marshalErr != nil {
			return marshalErr
		}
		if err = q.SaveModelPricingCatalog(ctx, payload); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if s.OnChange != nil {
		ids, listErr := s.queries.ListModelPricingWorkspaceIDs(ctx)
		if listErr != nil {
			return listErr
		}
		for _, id := range ids {
			s.OnChange(id.String())
		}
	}
	if refreshErr != nil {
		return fmt.Errorf("%w: %w", ErrRefresh, refreshErr)
	}
	return nil
}

// Midnight uses a calendar date, not a 24-hour UTC bucket (DST days can be
// 23 or 25 hours long). The scheduler shares this exact plan across replicas.
func Midnight(now time.Time, location *time.Location) time.Time {
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UTC()
}
