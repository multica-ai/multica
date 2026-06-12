package daemon

import "testing"

func TestProviderCLISourcesIncludesOfficialProviderRegistry(t *testing.T) {
	sources := ProviderCLISources()
	for _, provider := range []string{"codex", "kimi", "opencode", "gemini", "claude"} {
		source, ok := sources[provider]
		if !ok {
			t.Fatalf("missing provider CLI source for %s", provider)
		}
		if source.OfficialSourceURL == "" {
			t.Fatalf("%s official source URL is empty", provider)
		}
		if len(source.LatestVersionCommand) == 0 || len(source.UpgradeCommand) == 0 {
			t.Fatalf("%s source does not include version and upgrade commands: %+v", provider, source)
		}
	}
}

func TestPlanProviderCLIUpdateUsesPinnedVersionAndRollback(t *testing.T) {
	d := &Daemon{}
	plan := d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{
		RuntimeID:       "rt-codex",
		Provider:        "codex",
		CurrentVersion:  "0.125.0",
		TargetVersion:   "0.139.0",
		PinnedVersion:   "0.136.0",
		RollbackVersion: "0.125.0",
	})

	if !plan.DryRun {
		t.Fatal("provider CLI plan must be dry-run by default")
	}
	if !plan.CanStart {
		t.Fatalf("plan unexpectedly blocked: %s", plan.BlockedReason)
	}
	if plan.TargetVersion != "0.136.0" {
		t.Fatalf("target version = %q, want pinned version", plan.TargetVersion)
	}
	if plan.RollbackVersion != "0.125.0" {
		t.Fatalf("rollback version = %q", plan.RollbackVersion)
	}
	for _, phase := range plan.Phases {
		if phase.Execute {
			t.Fatalf("phase %s is executable in dry-run plan", phase.Name)
		}
	}
}

func TestPlanProviderCLIUpdateDefaultsRollbackToCurrentVersion(t *testing.T) {
	d := &Daemon{}
	plan := d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{
		Provider:       "kimi",
		CurrentVersion: "0.12.1",
		TargetVersion:  "0.14.2",
	})

	if plan.RollbackVersion != "0.12.1" {
		t.Fatalf("rollback version = %q, want current version", plan.RollbackVersion)
	}
}

func TestPlanProviderCLIUpdateBlocksWhenRuntimeBusy(t *testing.T) {
	d := &Daemon{}
	d.activeTasks.Store(1)

	plan := d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{
		Provider:       "opencode",
		CurrentVersion: "1.14.41",
		TargetVersion:  "1.17.4",
	})

	if plan.CanStart {
		t.Fatal("busy runtime should block provider CLI update")
	}
	if plan.BlockedReason != "runtime has 1 active task(s)" {
		t.Fatalf("blocked reason = %q", plan.BlockedReason)
	}
}

func TestPlanProviderCLIUpdateBlocksUnknownProvider(t *testing.T) {
	d := &Daemon{}
	plan := d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{
		Provider:      "unknown",
		TargetVersion: "1.2.3",
	})

	if plan.CanStart {
		t.Fatal("unknown provider should be blocked")
	}
	if plan.BlockedReason == "" {
		t.Fatal("missing blocked reason for unknown provider")
	}
}
