package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	antigravityQuotaProvider  = "antigravity"
	antigravityQuotaURL       = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
	antigravityOAuthTokenFile = agent.AntigravityOAuthTokenPath
)

var (
	errAntigravityQuotaToken   = errors.New("antigravity quota: OAuth token unavailable")
	errAntigravityQuotaAuth    = errors.New("antigravity quota: authorization rejected")
	errAntigravityQuotaVersion = errors.New("antigravity quota: CLI version unavailable")
	errAntigravityQuotaShape   = errors.New("antigravity quota: unusable response")
	antigravityCLIVersionRE    = regexp.MustCompile(`\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)*`)
)

// The CLI owns token refresh. Read its nested JSON token on every round so a
// remote-control refresh is picked up without restarting the daemon. Only the
// access token goes to Google's fixed HTTPS endpoint; no credentials or account
// metadata enter the heartbeat snapshot or error logs.
type antigravityPlanQuotaCollector struct {
	client   *http.Client
	homeDir  string
	endpoint string // fixed in production; overridable for HTTP fixture tests
}

func newAntigravityPlanQuotaCollector(homeDir string) *antigravityPlanQuotaCollector {
	return &antigravityPlanQuotaCollector{
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		homeDir:  homeDir,
		endpoint: antigravityQuotaURL,
	}
}

func (c *antigravityPlanQuotaCollector) readToken() (string, error) {
	raw, err := os.ReadFile(filepath.Join(c.homeDir, antigravityOAuthTokenFile))
	if err != nil {
		return "", fmt.Errorf("%w: cannot read token file", errAntigravityQuotaToken)
	}
	var envelope struct {
		Token struct {
			AccessToken string `json:"access_token"`
		} `json:"token"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", fmt.Errorf("%w: invalid token file", errAntigravityQuotaToken)
	}
	accessToken := strings.TrimSpace(envelope.Token.AccessToken)
	if accessToken == "" {
		return "", fmt.Errorf("%w: missing access token", errAntigravityQuotaToken)
	}
	return accessToken, nil
}

func (c *antigravityPlanQuotaCollector) collect(ctx context.Context, version string) (*protocol.RuntimePlanQuota, error) {
	// Discovery retains the full --version line (e.g. "agy version 1.3.0").
	// The licensing endpoint expects only the version token in this header.
	version = antigravityCLIVersionRE.FindString(version)
	if version == "" {
		return nil, errAntigravityQuotaVersion
	}
	token, err := c.readToken()
	if err != nil {
		return nil, err
	}
	// Verified against agy 1.4.2 (OL-846): the daily host and native
	// antigravity/cli identity select the client's current quota view.
	// The old host returns HTTP 200 with stale quotas; the old
	// antigravity-cli identity on the daily host omits Gemini entirely.
	// Keep the empty body (no project id) and never fall back to another
	// host or the legacy per-model endpoint on failure.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "antigravity/cli/"+version)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("antigravity quota request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return parseAntigravityQuotaSummary(io.LimitReader(resp.Body, 1<<20), time.Now())
	case http.StatusTooManyRequests:
		return nil, &rateLimitError{err: errors.New("antigravity quota: http 429")}
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("%w (http %d)", errAntigravityQuotaAuth, resp.StatusCode)
	default:
		return nil, fmt.Errorf("antigravity quota: unexpected http %d", resp.StatusCode)
	}
}

// The summary endpoint reports one bucket per quota pool and window type
// (OL-141 live capture against agy 1.3.0, matching the Antigravity client's
// own usage page). Every recognized bucket maps to exactly one window — no
// aggregation across buckets, no period inference. Unknown pools are skipped.
// A known pool with an invalid remaining fraction, a duplicate window name,
// or a result that fails the shared wire contract fails the whole round
// (fail-closed) so the platform keeps the last snapshot. A missing or
// unparsable resetTime on an otherwise valid bucket is deliberately
// tolerated: the window simply carries no countdown.
func parseAntigravityQuotaSummary(body io.Reader, observedAt time.Time) (*protocol.RuntimePlanQuota, error) {
	var response struct {
		Groups []struct {
			Buckets []antigravitySummaryBucket `json:"buckets"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		return nil, fmt.Errorf("%w: invalid json", errAntigravityQuotaShape)
	}
	quota := &protocol.RuntimePlanQuota{
		Provider:   antigravityQuotaProvider,
		Status:     protocol.PlanQuotaStatusOK,
		Source:     protocol.PlanQuotaSourceDaemon,
		ObservedAt: observedAt.Unix(),
	}
	seen := make(map[string]bool)
	for _, group := range response.Groups {
		for _, bucket := range group.Buckets {
			window, err := antigravityBucketWindow(bucket)
			if err != nil {
				return nil, err
			}
			if window == nil {
				continue
			}
			if seen[window.Name] {
				return nil, fmt.Errorf("%w: duplicate window %q", errAntigravityQuotaShape, window.Name)
			}
			seen[window.Name] = true
			if bucket.RemainingFraction != nil && *bucket.RemainingFraction == 0 {
				quota.Status = protocol.PlanQuotaStatusLimited
			}
			quota.Windows = append(quota.Windows, *window)
		}
	}
	if len(quota.Windows) == 0 {
		return nil, fmt.Errorf("%w: no recognized quota pool", errAntigravityQuotaShape)
	}
	// Stable display order: Gemini pool first, 5h before weekly inside a
	// pool, unrecognized buckets last.
	sort.SliceStable(quota.Windows, func(i, j int) bool {
		ri, rj := antigravityWindowRank(quota.Windows[i]), antigravityWindowRank(quota.Windows[j])
		if ri != rj {
			return ri < rj
		}
		return quota.Windows[i].Name < quota.Windows[j].Name
	})
	// Gate the result through the shared wire contract before returning:
	// drift the parser itself tolerates (e.g. an oversized legacy bucketId
	// used as a window name) must fail this round here, otherwise the
	// snapshot would be silently dropped downstream while /health records
	// a success.
	if err := protocol.ValidateRuntimePlanQuota(quota, observedAt); err != nil {
		return nil, fmt.Errorf("%w: %v", errAntigravityQuotaShape, err)
	}
	return quota, nil
}

type antigravitySummaryBucket struct {
	BucketID          string   `json:"bucketId"`
	Window            string   `json:"window"`
	RemainingFraction *float64 `json:"remainingFraction"`
	ResetTime         string   `json:"resetTime"`
}

// antigravityBucketWindow maps one summary bucket to its window. A nil window
// without error means the bucket belongs to no known pool and is skipped.
func antigravityBucketWindow(bucket antigravitySummaryBucket) (*protocol.RuntimePlanQuotaWindow, error) {
	var pool string
	switch {
	case strings.HasPrefix(bucket.BucketID, "gemini-"):
		pool = "gemini"
	case strings.HasPrefix(bucket.BucketID, "3p-"):
		pool = "claude_gpt"
	default:
		return nil, nil
	}
	if bucket.RemainingFraction == nil || !(*bucket.RemainingFraction >= 0 && *bucket.RemainingFraction <= 1) {
		return nil, fmt.Errorf("%w: bucket %q lacks a valid remaining fraction", errAntigravityQuotaShape, bucket.BucketID)
	}
	window := &protocol.RuntimePlanQuotaWindow{Group: pool}
	switch bucket.Window {
	case "5h":
		window.Name = pool + "_5h"
		minutes := int64(300)
		window.WindowMinutes = &minutes
	case "weekly":
		window.Name = pool + "_weekly"
		minutes := int64(10080)
		window.WindowMinutes = &minutes
	default:
		// Unrecognized or missing period: keep the bucket identifiable via
		// its raw id, never guess a length.
		window.Name = bucket.BucketID
	}
	used := math.Round((1-*bucket.RemainingFraction)*10000) / 100
	window.UsedPercent = &used
	// A fully replenished window reports a rolling "now + period" reset
	// that advances on every collection, so it carries no countdown;
	// anything the provider does disclose is parsed with fractional-second
	// tolerance and passed through verbatim.
	if *bucket.RemainingFraction < 1 {
		if reset, err := time.Parse(time.RFC3339Nano, bucket.ResetTime); err == nil {
			unix := reset.Unix()
			window.ResetsAt = &unix
		}
	}
	return window, nil
}

// Known windows order pool-then-period; anything unrecognized trails.
func antigravityWindowRank(window protocol.RuntimePlanQuotaWindow) int {
	pool, known := map[string]int{"gemini": 0, "claude_gpt": 1}[window.Group]
	if !known {
		return 4
	}
	switch strings.TrimPrefix(window.Name, window.Group+"_") {
	case "5h":
		return pool * 2
	case "weekly":
		return pool*2 + 1
	default:
		return 4
	}
}

func (d *Daemon) antigravityPlanQuotaLoop(ctx context.Context) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		d.logger.Debug("antigravity quota collector disabled: no home directory")
		return
	}
	d.runAntigravityPlanQuotaCollector(ctx, newAntigravityPlanQuotaCollector(homeDir), defaultPlanQuotaPollInterval)
}

func (d *Daemon) runAntigravityPlanQuotaCollector(ctx context.Context, collector *antigravityPlanQuotaCollector, interval time.Duration) {
	d.runPlanQuotaCollector(ctx, antigravityQuotaProvider, interval, func() []string {
		return d.runtimeIDsForProvider(antigravityQuotaProvider)
	}, func(ctx context.Context) (*protocol.RuntimePlanQuota, error) {
		entry, ok := d.agents()[antigravityQuotaProvider]
		if !ok || entry.Path == "" {
			d.recordAntigravityQuotaSkip(antigravityQuotaSkipNotRegistered)
			return nil, nil
		}
		version := d.agentVersion(antigravityQuotaProvider)
		if version == "" {
			d.recordAntigravityQuotaSkip(antigravityQuotaSkipNoVersion)
			return nil, nil
		}
		quota, err := collector.collect(ctx, version)
		if err != nil {
			d.recordAntigravityQuotaSkip(antigravityQuotaSkipReasonFor(err))
		} else {
			d.recordAntigravityQuotaSuccess()
		}
		return quota, err
	})
}
