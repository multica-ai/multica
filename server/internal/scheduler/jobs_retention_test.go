package scheduler

import (
	"testing"
)

func TestRetentionConfigFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want retentionConfig
	}{
		{
			name: "unset means fully disabled",
			env:  map[string]string{},
			want: retentionConfig{CronExecutionsDays: 0, WebhookDeliveryDay: 0, InboxItemDays: 0},
		},
		{
			name: "shared default enables all tables",
			env:  map[string]string{"MULTICA_RETENTION_DAYS": "90"},
			want: retentionConfig{CronExecutionsDays: 90, WebhookDeliveryDay: 90, InboxItemDays: 90},
		},
		{
			name: "per-table override wins",
			env: map[string]string{
				"MULTICA_RETENTION_DAYS":                  "90",
				"MULTICA_RETENTION_WEBHOOK_DELIVERY_DAYS": "14",
				"MULTICA_RETENTION_INBOX_ITEM_DAYS":       "0",
			},
			want: retentionConfig{CronExecutionsDays: 90, WebhookDeliveryDay: 14, InboxItemDays: 0},
		},
		{
			name: "invalid value falls back to default",
			env: map[string]string{
				"MULTICA_RETENTION_DAYS":                 "ninety",
				"MULTICA_RETENTION_CRON_EXECUTIONS_DAYS": "-5",
			},
			want: retentionConfig{CronExecutionsDays: 0, WebhookDeliveryDay: 0, InboxItemDays: 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(name string) string { return tc.env[name] }
			got := retentionConfigFromEnv(getenv)
			if got != tc.want {
				t.Errorf("retentionConfigFromEnv() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRetentionTablesDisabledAreDropped(t *testing.T) {
	cfg := retentionConfig{CronExecutionsDays: 90, WebhookDeliveryDay: 0, InboxItemDays: 30}
	tables := retentionTables(cfg)
	if len(tables) != 2 {
		t.Fatalf("len(tables) = %d, want 2 (webhook_delivery disabled)", len(tables))
	}
	for _, tb := range tables {
		if tb.name == "webhook_delivery" {
			t.Errorf("disabled table %q still enabled", tb.name)
		}
	}
}
