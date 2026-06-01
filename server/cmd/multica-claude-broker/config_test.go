package main

import (
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "multica")

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Namespace != "multica" {
		t.Errorf("Namespace = %q", got.Namespace)
	}
	if got.SecretName != "multica-claude-oauth-broker" {
		t.Errorf("SecretName = %q", got.SecretName)
	}
	if got.RefreshPad != 5*time.Minute {
		t.Errorf("RefreshPad default = %v", got.RefreshPad)
	}
	if got.RefreshInterval != 60*time.Second {
		t.Errorf("RefreshInterval default = %v", got.RefreshInterval)
	}
	if got.LeaseName != "multica-claude-broker-refresh" {
		t.Errorf("LeaseName default = %q", got.LeaseName)
	}
	if got.AdminAddr != ":8080" {
		t.Errorf("AdminAddr default = %q", got.AdminAddr)
	}
	if got.OpsAddr != "127.0.0.1:8081" {
		t.Errorf("OpsAddr default = %q (must be loopback-only)", got.OpsAddr)
	}
	if got.MetricsAddr != ":9090" {
		t.Errorf("MetricsAddr default = %q", got.MetricsAddr)
	}
}

func TestLoadConfig_RequiresNamespace(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "")
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error for missing POD_NAMESPACE")
	}
}

func TestLoadConfig_Overrides(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "ns")
	t.Setenv("BROKER_SECRET_NAME", "custom-secret")
	t.Setenv("BROKER_REFRESH_PAD", "2m30s")
	t.Setenv("BROKER_REFRESH_INTERVAL", "15s")
	t.Setenv("BROKER_LEASE_NAME", "custom-lease")
	t.Setenv("BROKER_ADMIN_ADDR", ":9000")
	t.Setenv("BROKER_OPS_ADDR", "127.0.0.1:9001")
	t.Setenv("BROKER_METRICS_ADDR", ":9002")
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.SecretName != "custom-secret" {
		t.Errorf("SecretName override = %q", got.SecretName)
	}
	if got.RefreshPad != 2*time.Minute+30*time.Second {
		t.Errorf("RefreshPad override = %v", got.RefreshPad)
	}
	if got.RefreshInterval != 15*time.Second {
		t.Errorf("RefreshInterval override = %v", got.RefreshInterval)
	}
	if got.LeaseName != "custom-lease" {
		t.Errorf("LeaseName override = %q", got.LeaseName)
	}
	if got.AdminAddr != ":9000" {
		t.Errorf("AdminAddr override = %q", got.AdminAddr)
	}
	if got.OpsAddr != "127.0.0.1:9001" {
		t.Errorf("OpsAddr override = %q", got.OpsAddr)
	}
	if got.MetricsAddr != ":9002" {
		t.Errorf("MetricsAddr override = %q", got.MetricsAddr)
	}
}

func TestLoadConfig_BadDuration(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "ns")
	t.Setenv("BROKER_REFRESH_PAD", "not-a-duration")
	_, err := LoadConfig()
	if err == nil {
		t.Fatal("expected error for bad duration")
	}
}
