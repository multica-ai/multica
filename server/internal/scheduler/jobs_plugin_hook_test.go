package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestPluginHookSchedulePlansCollapseToLatestAndStaySerial(t *testing.T) {
	cache := newPluginHookScheduleCache()
	activated := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	scope := Scope{Kind: ScopeKindPluginHookSchedule, ID: "schedule:generation"}
	cache.replace(map[string]pluginHookScheduleConfig{
		scope.ID: {
			CronExpression: "*/5 * * * *",
			Timezone:       "UTC",
			ActivatedAt:    activated,
		},
	})
	plans := pluginHookSchedulePlans(cache)
	now := activated.Add(16 * time.Minute)

	got, err := plans(context.Background(), scope, now, LatestPlanInfo{})
	if err != nil {
		t.Fatalf("plan initial occurrence: %v", err)
	}
	if len(got) != 1 || !got[0].Equal(activated.Add(15*time.Minute)) {
		t.Fatalf("got %v, want only the latest 10:15 occurrence", got)
	}
	if count := cache.takeCoalesced(scope.ID, got[0]); count != 2 {
		t.Fatalf("coalesced=%d, want 2 older due occurrences", count)
	}

	latest := LatestPlanInfo{
		Found:       true,
		PlanTime:    activated.Add(5 * time.Minute),
		Status:      "RUNNING",
		Attempt:     1,
		MaxAttempts: pluginHookScheduleAttempts,
	}
	got, err = plans(context.Background(), scope, now, latest)
	if err != nil {
		t.Fatalf("plan while running: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a running latest plan must block newer plans, got %v", got)
	}

	latest.Status = "FAILED"
	latest.NextRetryAt = now.Add(time.Minute)
	got, err = plans(context.Background(), scope, now, latest)
	if err != nil {
		t.Fatalf("plan before retry: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a retryable failure must block newer plans before backoff, got %v", got)
	}

	latest.NextRetryAt = now.Add(-time.Second)
	got, err = plans(context.Background(), scope, now, latest)
	if err != nil {
		t.Fatalf("plan due retry: %v", err)
	}
	if len(got) != 1 || !got[0].Equal(latest.PlanTime) {
		t.Fatalf("retry must reuse plan_time %s, got %v", latest.PlanTime, got)
	}
}

func TestLatestPluginHookOccurrencePagesThroughLongOutage(t *testing.T) {
	activated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := activated.Add(9 * 24 * time.Hour)
	got, count, err := latestPluginHookOccurrence("*/5 * * * *", "UTC", activated, now)
	if err != nil {
		t.Fatalf("enumerate occurrences: %v", err)
	}
	if !got.Equal(now) {
		t.Fatalf("latest=%s, want %s", got, now)
	}
	if count != 9*24*12 {
		t.Fatalf("count=%d, want %d", count, 9*24*12)
	}
}

func TestPluginHookScheduleDeliveryIDIsStablePerOccurrence(t *testing.T) {
	installation := util.MustParseUUID("11111111-1111-4111-8111-111111111111")
	generation := util.MustParseUUID("22222222-2222-4222-8222-222222222222")
	plan := time.Date(2026, 8, 23, 10, 15, 0, 0, time.UTC)

	first := pluginHookScheduleDeliveryID(installation, "digest", generation, plan)
	second := pluginHookScheduleDeliveryID(installation, "digest", generation, plan)
	if first != second {
		t.Fatalf("same occurrence changed delivery id: %q != %q", first, second)
	}
	if first == pluginHookScheduleDeliveryID(installation, "digest", generation, plan.Add(5*time.Minute)) {
		t.Fatal("different plan_time must produce a different delivery id")
	}
	if first == pluginHookScheduleDeliveryID(
		installation,
		"digest",
		util.MustParseUUID("33333333-3333-4333-8333-333333333333"),
		plan,
	) {
		t.Fatal("different generation must produce a different delivery id")
	}
}

func TestPluginHookScheduleHandlerRechecksFeatureFlagAfterClaim(t *testing.T) {
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.PluginsV1, featureflag.Rule{Default: false})
	plugins := &service.PluginService{FeatureFlags: featureflag.NewService(provider)}
	handler := pluginHookScheduleHandler(nil, plugins, newPluginHookScheduleCache())

	result, err := handler(context.Background(), HandlerInput{
		Scope: Scope{ID: "11111111-1111-4111-8111-111111111111:22222222-2222-4222-8222-222222222222"},
	})
	if err != nil {
		t.Fatalf("feature-disabled handler: %v", err)
	}
	if result.Result["skipped_reason"] != "feature_disabled" {
		t.Fatalf("skipped_reason=%v, want feature_disabled", result.Result["skipped_reason"])
	}
}
