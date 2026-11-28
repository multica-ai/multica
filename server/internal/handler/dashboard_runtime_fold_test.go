package handler

import (
	"reflect"
	"testing"
)

func TestFoldRestrictedUsageByRuntime(t *testing.T) {
	restricted := map[string]struct{}{"priv-a": {}, "priv-b": {}}
	rows := []DashboardUsageByRuntimeResponse{
		{RuntimeID: "pub", Provider: "anthropic", Model: "claude", InputTokens: 1},
		{RuntimeID: "priv-a", Provider: "anthropic", Model: "claude", InputTokens: 10, CostUSDTicks: 5, UncostedInputTokens: 2},
		{RuntimeID: "priv-b", Provider: "anthropic", Model: "claude", InputTokens: 20, CostUSDTicks: 7, UncostedInputTokens: 3},
		{RuntimeID: "priv-b", Provider: "openai", Model: "gpt", InputTokens: 40},
	}
	want := []DashboardUsageByRuntimeResponse{
		{RuntimeID: "pub", Provider: "anthropic", Model: "claude", InputTokens: 1},
		{RuntimeID: restrictedRuntimesRowID, Provider: "anthropic", Model: "claude", InputTokens: 30, CostUSDTicks: 12, UncostedInputTokens: 5},
		{RuntimeID: restrictedRuntimesRowID, Provider: "openai", Model: "gpt", InputTokens: 40},
	}
	if got := foldRestrictedUsageByRuntime(rows, restricted); !reflect.DeepEqual(got, want) {
		t.Fatalf("fold =\n%+v\nwant\n%+v", got, want)
	}
}

func TestFoldRestrictedRuntimeDuration(t *testing.T) {
	restricted := map[string]struct{}{"priv-a": {}, "priv-b": {}}
	rows := []DashboardRuntimeDurationResponse{
		{RuntimeID: "priv-a", TotalSeconds: 10, TaskCount: 1, MeteredTaskCount: 1, FailedCount: 1},
		{RuntimeID: "pub", TotalSeconds: 5, TaskCount: 1},
		{RuntimeID: "priv-b", TotalSeconds: 20, TaskCount: 2, CancelledCount: 1},
	}
	want := []DashboardRuntimeDurationResponse{
		{RuntimeID: restrictedRuntimesRowID, TotalSeconds: 30, TaskCount: 3, MeteredTaskCount: 1, FailedCount: 1, CancelledCount: 1},
		{RuntimeID: "pub", TotalSeconds: 5, TaskCount: 1},
	}
	if got := foldRestrictedRuntimeDuration(rows, restricted); !reflect.DeepEqual(got, want) {
		t.Fatalf("fold =\n%+v\nwant\n%+v", got, want)
	}
}
