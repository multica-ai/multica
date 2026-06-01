package identitysync

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// defaultInterval is the FIR-2570 cadence ("baggrundstjek hver 4. time").
const defaultInterval = 4 * time.Hour

// config holds the BigQuery connection details for the sync worker. All of it
// comes from the environment so a self-host install without a data lake never
// configures (and never runs) the worker.
type config struct {
	// Project is the GCP project the BigQuery client bills against and the
	// project that owns the group_members table.
	Project string
	Dataset string
	Table   string
	// CredentialJSON is an optional service-account key (raw JSON). When
	// empty, the client falls back to Application Default Credentials.
	CredentialJSON string
	Interval       time.Duration
}

// loadConfig reads the worker config from the environment. ok=false means the
// worker is not configured (no BigQuery project) and must not start.
func loadConfig() (config, bool) {
	project := strings.TrimSpace(os.Getenv("CEREBRO_GWS_SYNC_BQ_PROJECT"))
	if project == "" {
		return config{}, false
	}
	cfg := config{
		Project:        project,
		Dataset:        envOr("CEREBRO_GWS_SYNC_BQ_DATASET", "google_admin"),
		Table:          envOr("CEREBRO_GWS_SYNC_BQ_TABLE", "group_members"),
		CredentialJSON: strings.TrimSpace(os.Getenv("CEREBRO_GWS_SYNC_BQ_CREDENTIALS_JSON")),
		Interval:       defaultInterval,
	}
	if v := strings.TrimSpace(os.Getenv("CEREBRO_GWS_SYNC_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Interval = d
		} else {
			slog.Warn("gws group sync: invalid CEREBRO_GWS_SYNC_INTERVAL, using default",
				"value", v, "default", defaultInterval.String())
		}
	}
	return cfg, true
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
