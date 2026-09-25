package lark

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
)

// BackfillRegionFromLegacyOverride is the upgrade-repair path for self-host
// deployments that ran the WHOLE Lark integration against Lark international
// via the deployment-wide MULTICA_LARK_HTTP_BASE_URL /
// MULTICA_LARK_CALLBACK_BASE_URL override, before per-installation region
// existed.
//
// Migration 116 backfilled every existing row to 'feishu' (the mainland
// default). On such a deployment every install is really Lark, so once
// region drives the host those rows would route to open.feishu.cn and
// break the moment the operator clears the override (which the new docs
// invite them to do). When either override host is the Lark international
// host, we flip the still-default rows to 'lark'.
//
// Gating on the override is what makes this safe: the override was
// deployment-wide, so EVERY pre-existing install on it was Lark — there is
// no mixed state to misclassify. It is idempotent (after the flip nothing
// remains at 'feishu'), and new installs already carry the device-flow-
// detected region, so this only ever touches the legacy rows. Mainland
// deployments (no override, or override pointing at open.feishu.cn / a
// mock) never run the UPDATE.
//
// Callers should fire this from a goroutine at boot, like
// BackfillBotUnionIDs, so a slow DB write cannot block listener startup.
func BackfillRegionFromLegacyOverride(ctx context.Context, queries *ChannelStore, httpOverride, callbackOverride string, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	if queries == nil {
		return
	}
	if !isLarkInternationalHost(httpOverride) && !isLarkInternationalHost(callbackOverride) {
		// No override, mainland override, or a mock/staging host: the
		// migration's 'feishu' default is correct for these rows.
		return
	}
	n, err := queries.BackfillLarkInstallationRegionToLark(ctx)
	if err != nil {
		log.Warn("lark region backfill: relabel legacy Lark-international installs failed", "err", err)
		return
	}
	if n > 0 {
		log.Info("lark region backfill: relabelled legacy Lark-international installs to region=lark",
			"rows", n)
	}
}

// isLarkInternationalHost reports whether a configured base-URL override
// targets the Lark international open-platform host (open.larksuite.com).
// It parses the URL and matches the host exactly so a mainland host, an
// empty value, or a staging/mock URL never triggers the upgrade relabel.
func isLarkInternationalHost(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, "open.larksuite.com")
}
