package logger

import "testing"

func TestConfigFromEnvDefaultsToSafeSummary(t *testing.T) {
	t.Setenv("LOG_REQUEST_MODE", "")
	t.Setenv("LOG_SQL_DETAIL", "")
	t.Setenv("LOG_RESPONSE_DETAIL", "")

	cfg := ConfigFromEnv()
	if cfg.RequestMode != RequestLogSummary {
		t.Fatalf("expected summary request mode, got %q", cfg.RequestMode)
	}
	if cfg.SQLDetail {
		t.Fatal("expected SQL detail to default to false")
	}
	if cfg.ResponseDetail {
		t.Fatal("expected response detail to default to false")
	}
}

func TestConfigFromEnvParsesExplicitValues(t *testing.T) {
	t.Setenv("LOG_REQUEST_MODE", "enhanced")
	t.Setenv("LOG_SQL_DETAIL", "true")
	t.Setenv("LOG_RESPONSE_DETAIL", "1")

	cfg := ConfigFromEnv()
	if cfg.RequestMode != RequestLogEnhanced {
		t.Fatalf("expected enhanced request mode, got %q", cfg.RequestMode)
	}
	if !cfg.SQLDetail {
		t.Fatal("expected SQL detail to be true")
	}
	if !cfg.ResponseDetail {
		t.Fatal("expected response detail to be true")
	}
}

func TestParseRequestLogModeFallsBackToSummary(t *testing.T) {
	if got := ParseRequestLogMode("unknown"); got != RequestLogSummary {
		t.Fatalf("expected invalid mode to fall back to summary, got %q", got)
	}
}
