package qoderruntime

import (
	"context"
	"math"
	"net/http"
	"net/url"
)

// usageProvider is the task_usage provider for Qoder Cloud Agents runs; it
// matches the runtime provider so usage rows line up with the runtime.
const usageProvider = "qoder_cloud"

// runCredits tracks what Qoder charged for one run, in millionths of a credit.
// Qoder reports credits instead of tokens. A session is cumulative across
// continued runs, so Base is the session's total when this run took it over.
type runCredits struct {
	Base        *int64 `json:"base,omitempty"`
	Spans       int64  `json:"spans,omitempty"`
	SpansSeen   bool   `json:"spans_seen,omitempty"`
	Session     *int64 `json:"session,omitempty"`
	ReportedFor string `json:"reported_for,omitempty"`
}

func creditsMicros(credits float64) int64 {
	if credits <= 0 || math.IsNaN(credits) || math.IsInf(credits, 0) {
		return 0
	}
	return int64(math.Round(credits * 1e6))
}

func (r *run) addSpanCredits(credits float64) {
	r.Credits.Spans += creditsMicros(credits)
	r.Credits.SpansSeen = true
}

// setSessionCredits records a session total. Totals are snapshots, so the
// latest one replaces the previous figure instead of adding to it.
func (r *run) setSessionCredits(total float64) {
	// A missing baseline is not zero. This also covers journals written
	// before credit collection: charging the session total would bill history.
	if r.Continued && r.Credits.Base == nil {
		r.Credits.Session = nil
		return
	}
	var base int64
	if r.Credits.Base != nil {
		base = *r.Credits.Base
	}
	v := max(creditsMicros(total)-base, 0)
	r.Credits.Session = &v
}

type sessionUsage struct {
	Usage *struct {
		TotalCredits *float64 `json:"total_credits"`
	} `json:"usage"`
}

func (s sessionUsage) total() *float64 {
	if s.Usage == nil {
		return nil
	}
	return s.Usage.TotalCredits
}

// reportCredits sends the run's credit charge to Multica before the task is
// finished. The session total is authoritative because span events do not
// cover every billed call (sandbox runtime among them); spans are only the
// fallback when Qoder exposes no total or a continued run has no baseline.
// This is a usage snapshot, not a final invoice: sandbox charges settled
// after this read are not included and may enter a later run's baseline.
func (b *Bridge) reportCredits(ctx context.Context, r *run) error {
	if r.SessionID == "" || r.Credits.ReportedFor == r.SessionID {
		return nil
	}
	var s sessionUsage
	if err := b.qoder.call(ctx, http.MethodGet, "/sessions/"+url.PathEscape(r.SessionID), nil, &s); err != nil {
		b.log.WarnContext(ctx, "cannot read Qoder session usage", "task_id", r.TaskID, "error", err)
	} else if total := s.total(); total != nil {
		r.setSessionCredits(*total)
	}
	var credits int64
	switch {
	case r.Credits.Session != nil:
		credits = *r.Credits.Session
	case r.Credits.SpansSeen:
		credits = r.Credits.Spans
	default:
		return nil
	}
	body := map[string]any{"usage": []map[string]any{{
		"provider":       usageProvider,
		"model":          "",
		"credits_micros": credits,
	}}}
	if err := b.taskCall(ctx, r, "/usage", body, nil); err != nil {
		if !definitelyRejected(err) {
			return err
		}
		b.log.WarnContext(ctx, "Multica rejected Qoder credits usage", "task_id", r.TaskID, "error", err)
	}
	r.Credits.ReportedFor = r.SessionID
	return b.persist()
}
