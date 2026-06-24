package service

import (
	"strings"
	"testing"
	"time"
)

func TestValidateCronMinInterval(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		floor   time.Duration
		wantErr bool
	}{
		{name: "every minute rejected at default floor", expr: "* * * * *", floor: 5 * time.Minute, wantErr: true},
		{name: "every two minutes rejected at default floor", expr: "*/2 * * * *", floor: 5 * time.Minute, wantErr: true},
		{name: "every five minutes accepted at default floor", expr: "*/5 * * * *", floor: 5 * time.Minute, wantErr: false},
		{name: "hourly accepted", expr: "0 * * * *", floor: 5 * time.Minute, wantErr: false},
		{name: "daily accepted", expr: "0 9 * * *", floor: 5 * time.Minute, wantErr: false},
		{name: "bursty minute list rejected", expr: "0,1,2 9 * * *", floor: 5 * time.Minute, wantErr: true},
		{name: "zero floor disables check", expr: "* * * * *", floor: 0, wantErr: false},
		{name: "invalid expression returns error", expr: "not a cron", floor: 5 * time.Minute, wantErr: true},
		{name: "stricter floor rejects hourly", expr: "0 * * * *", floor: 2 * time.Hour, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCronMinInterval(tt.expr, "UTC", tt.floor)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateCronMinInterval(%q, UTC, %s) error = %v, wantErr %v", tt.expr, tt.floor, err, tt.wantErr)
			}
		})
	}
}

func TestValidateCronMinIntervalInvalidTimezone(t *testing.T) {
	err := validateCronMinInterval("0 * * * *", "Not/AZone", 5*time.Minute)
	if err == nil || !strings.Contains(err.Error(), "invalid timezone") {
		t.Fatalf("expected invalid timezone error, got %v", err)
	}
}

func TestMinTriggerIntervalEnvOverride(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		{name: "unset uses default", env: "", want: DefaultMinTriggerInterval},
		{name: "custom duration", env: "1m", want: time.Minute},
		{name: "explicit zero disables", env: "0", want: 0},
		{name: "negative disables", env: "-5m", want: 0},
		{name: "garbage falls back to default", env: "soon", want: DefaultMinTriggerInterval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUTOPILOT_MIN_TRIGGER_INTERVAL", tt.env)
			if got := MinTriggerInterval(); got != tt.want {
				t.Fatalf("MinTriggerInterval() with env %q = %s, want %s", tt.env, got, tt.want)
			}
		})
	}
}
