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
		if len(source.LatestVersionCommandTemplate) == 0 || len(source.UpgradeCommandTemplate) == 0 {
			t.Fatalf("%s source does not include version and upgrade commands: %+v", provider, source)
		}
	}
}

func TestProviderCLISourcesDeepCopiesCommandTemplates(t *testing.T) {
	sources := ProviderCLISources()
	codex := sources["codex"]
	codex.LatestVersionCommandTemplate[0] = "mutated"
	codex.VersionCommandTemplate[0] = "mutated"
	codex.UpgradeCommandTemplate[0] = "mutated"
	sources["codex"] = codex

	fresh := ProviderCLISources()["codex"]
	if fresh.LatestVersionCommandTemplate[0] != "npm" {
		t.Fatalf("latest version command template was mutated: %+v", fresh.LatestVersionCommandTemplate)
	}
	if fresh.VersionCommandTemplate[0] != "codex" {
		t.Fatalf("version command template was mutated: %+v", fresh.VersionCommandTemplate)
	}
	if fresh.UpgradeCommandTemplate[0] != "npm" {
		t.Fatalf("upgrade command template was mutated: %+v", fresh.UpgradeCommandTemplate)
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
	if !plan.Valid {
		t.Fatalf("plan unexpectedly invalid: %s", plan.InvalidReason)
	}
	if !plan.ObservedIdle {
		t.Fatalf("plan unexpectedly observed busy runtime: %s", plan.PlanWarning)
	}
	if plan.TargetVersion != "0.136.0" {
		t.Fatalf("target version = %q, want pinned version", plan.TargetVersion)
	}
	if plan.RollbackVersion != "0.125.0" {
		t.Fatalf("rollback version = %q", plan.RollbackVersion)
	}
	for _, phase := range plan.Phases {
		if phase.Name == "upgrade_provider_cli" {
			t.Fatalf("phase %s looks like a real execution step", phase.Name)
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

	if !plan.Valid {
		t.Fatalf("busy runtime should not invalidate plan shape: %s", plan.InvalidReason)
	}
	if plan.ObservedIdle {
		t.Fatal("busy runtime should not be observed as idle")
	}
	if plan.PlanWarning != "runtime has 1 active task(s)" {
		t.Fatalf("plan warning = %q", plan.PlanWarning)
	}
}

func TestPlanProviderCLIUpdateBlocksUnknownProvider(t *testing.T) {
	d := &Daemon{}
	plan := d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{
		Provider:      "unknown",
		TargetVersion: "1.2.3",
	})

	if plan.Valid {
		t.Fatal("unknown provider should be invalid")
	}
	if plan.InvalidReason == "" {
		t.Fatal("missing invalid reason for unknown provider")
	}
}

func TestPlanProviderCLIUpdateUsesCommandTemplatesOnly(t *testing.T) {
	d := &Daemon{}
	plan := d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{
		Provider:      "codex",
		TargetVersion: "0.139.0",
	})

	var sawUpgradeTemplate bool
	for _, phase := range plan.Phases {
		if phase.Name != "upgrade_provider_cli_template" {
			continue
		}
		sawUpgradeTemplate = true
		if len(phase.CommandTemplate) == 0 {
			t.Fatal("upgrade template phase has no command template")
		}
		if phase.CommandTemplate[len(phase.CommandTemplate)-1] != "@openai/codex@<version>" {
			t.Fatalf("upgrade command template = %+v", phase.CommandTemplate)
		}
	}
	if !sawUpgradeTemplate {
		t.Fatal("missing upgrade command template phase")
	}
}
