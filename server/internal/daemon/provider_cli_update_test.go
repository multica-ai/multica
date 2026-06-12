package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

func TestMaterializeProviderCLICommandReplacesVersionAndInstallPrefix(t *testing.T) {
	got := materializeProviderCLICommand(
		[]string{"npm", "install", "-g", "--prefix", "<install_prefix>", "@openai/codex@<version>"},
		"0.139.0",
		"/usr/local",
	)
	want := []string{"npm", "install", "-g", "--prefix", "/usr/local", "@openai/codex@0.139.0"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("command = %+v, want %+v", got, want)
	}
}

func TestProviderCLIInstallLocationUsesDaemonPathPrefix(t *testing.T) {
	root := t.TempDir()
	daemonPath := filepath.Join(root, "bin", "codex")
	path, prefix, err := providerCLIInstallLocation(daemonPath, "")
	if err != nil {
		t.Fatalf("install location: %v", err)
	}
	if path != daemonPath || prefix != root {
		t.Fatalf("install location = (%q, %q)", path, prefix)
	}

	path, prefix, err = providerCLIInstallLocation("/opt/codex", "")
	if err != nil {
		t.Fatalf("non-bin install location: %v", err)
	}
	if path != "/opt/codex" || prefix != "/opt" {
		t.Fatalf("non-bin install location = (%q, %q)", path, prefix)
	}
}

func TestTimeOfDayInWindowHandlesNormalAndOvernightWindows(t *testing.T) {
	inside := time.Date(2026, 6, 12, 4, 30, 0, 0, time.UTC)
	outside := time.Date(2026, 6, 12, 7, 0, 0, 0, time.UTC)
	if !timeOfDayInWindow(inside, 4*time.Hour, 2*time.Hour) {
		t.Fatal("04:30 should be inside 04:00-06:00")
	}
	if timeOfDayInWindow(outside, 4*time.Hour, 2*time.Hour) {
		t.Fatal("07:00 should be outside 04:00-06:00")
	}

	overnight := time.Date(2026, 6, 12, 1, 0, 0, 0, time.UTC)
	if !timeOfDayInWindow(overnight, 23*time.Hour, 3*time.Hour) {
		t.Fatal("01:00 should be inside 23:00-02:00 overnight window")
	}
}

func TestParseProviderCLIUpdateModeDefaultsToDryRun(t *testing.T) {
	mode, err := parseProviderCLIUpdateMode("")
	if err != nil {
		t.Fatalf("parse default mode: %v", err)
	}
	if mode != ProviderCLIUpdateDryRun {
		t.Fatalf("mode = %q", mode)
	}
	for _, value := range []string{"true", "1", "yes", "on"} {
		mode, err = parseProviderCLIUpdateMode(value)
		if err != nil || mode != ProviderCLIUpdateDryRun {
			t.Fatalf("parse %s = %q, %v; want dry-run", value, mode, err)
		}
	}
	mode, err = parseProviderCLIUpdateMode("apply")
	if err != nil || mode != ProviderCLIUpdateApply {
		t.Fatalf("parse apply = %q, %v", mode, err)
	}
}

func TestBuildProviderCLIAutoUpdatePlanUsesPinnedWithoutLatestLookup(t *testing.T) {
	root := t.TempDir()
	called := false
	prev := providerCLICommandRunner
	providerCLICommandRunner = func(context.Context, []string) (string, error) {
		called = true
		return "0.139.0", nil
	}
	t.Cleanup(func() { providerCLICommandRunner = prev })

	d := &Daemon{
		cfg: Config{
			Agents:                    map[string]AgentEntry{"codex": {Path: filepath.Join(root, "bin", "codex")}},
			ProviderCLIPinnedVersions: map[string]string{"codex": "0.136.0"},
		},
		agentVersions: map[string]string{"codex": "0.125.0"},
	}
	plan, err := d.buildProviderCLIAutoUpdatePlan(context.Background(), "codex", d.cfg.Agents["codex"], ProviderCLIUpdateDryRun)
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if called {
		t.Fatal("latest lookup should be skipped when pinned version is configured")
	}
	if plan.TargetVersion != "0.136.0" || plan.RollbackVersion != "0.125.0" {
		t.Fatalf("plan target/rollback = %q/%q", plan.TargetVersion, plan.RollbackVersion)
	}
	if plan.InstallPrefix != root {
		t.Fatalf("install prefix = %q, want %q", plan.InstallPrefix, root)
	}
}

func TestApplyProviderCLIUpdateRequiresApplyModeAndIdleBarrier(t *testing.T) {
	d := &Daemon{cfg: Config{ProviderCLIUpdateMode: ProviderCLIUpdateDryRun}}
	plan := d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{
		Provider:      "codex",
		TargetVersion: "0.139.0",
		InstallPrefix: "/usr/local",
	})
	if err := d.applyProviderCLIUpdate(context.Background(), plan); err == nil {
		t.Fatal("dry-run mode must not apply provider CLI update")
	}

	d = &Daemon{cfg: Config{ProviderCLIUpdateMode: ProviderCLIUpdateApply, ProviderCLIUpdateWindowStartConfigured: true, ProviderCLIUpdateWindowDuration: 24 * time.Hour}}
	d.activeTasks.Store(1)
	if err := d.applyProviderCLIUpdate(context.Background(), plan); err == nil {
		t.Fatal("busy daemon must not apply provider CLI update")
	}
}

func TestApplyProviderCLIUpdateInstallFailureRecordsRollbackRequired(t *testing.T) {
	prev := providerCLICommandRunner
	providerCLICommandRunner = func(context.Context, []string) (string, error) {
		return "install output", context.Canceled
	}
	t.Cleanup(func() { providerCLICommandRunner = prev })

	d := &Daemon{cfg: Config{
		WorkspacesRoot:                         t.TempDir(),
		ProviderCLIUpdateMode:                  ProviderCLIUpdateApply,
		ProviderCLIUpdateWindowStartConfigured: true,
		ProviderCLIUpdateWindowDuration:        24 * time.Hour,
	}}
	plan := d.PlanProviderCLIUpdate(ProviderCLIUpdateRequest{
		Provider:        "codex",
		TargetVersion:   "0.139.0",
		RollbackVersion: "0.125.0",
		InstallPath:     "/usr/local/bin/codex",
		InstallPrefix:   "/usr/local",
		Mode:            string(ProviderCLIUpdateApply),
	})
	if err := d.applyProviderCLIUpdate(context.Background(), plan); err == nil {
		t.Fatal("install failure should return an error")
	}
	records, err := d.loadProviderCLIUpdateRecords()
	if err != nil {
		t.Fatalf("load records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	for _, record := range records {
		if record.Status != providerCLIUpdateRollbackRequired {
			t.Fatalf("status = %q, want rollback_required", record.Status)
		}
		if record.TargetVersion != "0.139.0" || record.RollbackVersion != "0.125.0" || record.UpdateID == "" {
			t.Fatalf("record did not persist target/rollback/update id: %+v", record)
		}
	}
}

func TestProviderCLIInstallLocationBlocksVersionManagerShimWithoutExplicitPrefix(t *testing.T) {
	_, _, err := providerCLIInstallLocation("/home/me/.volta/bin/codex", "")
	if err == nil {
		t.Fatal("volta shim should require explicit install prefix")
	}
	path, prefix, err := providerCLIInstallLocation("/home/me/.volta/bin/codex", "/usr/local")
	if err != nil {
		t.Fatalf("explicit prefix should allow shim path: %v", err)
	}
	if path == "" || prefix != "/usr/local" {
		t.Fatalf("explicit-prefix install location = (%q, %q)", path, prefix)
	}
}

func TestVerifyPendingProviderCLIUpdatesMarksVerified(t *testing.T) {
	d := &Daemon{
		cfg:           Config{WorkspacesRoot: t.TempDir()},
		agentVersions: map[string]string{"codex": "codex 0.139.0"},
	}
	record := providerCLIUpdateRecord{
		UpdateID:      "upd-1",
		Provider:      "codex",
		TargetVersion: "0.139.0",
		Status:        providerCLIUpdatePendingVerify,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := d.saveProviderCLIUpdateRecord(record); err != nil {
		t.Fatalf("save pending record: %v", err)
	}
	if err := d.verifyPendingProviderCLIUpdates(); err != nil {
		t.Fatalf("verify pending: %v", err)
	}
	records, err := d.loadProviderCLIUpdateRecords()
	if err != nil {
		t.Fatalf("load records: %v", err)
	}
	if got := records["upd-1"].Status; got != providerCLIUpdateVerified {
		t.Fatalf("status = %q, want verified", got)
	}
}

func TestVerifyPendingProviderCLIUpdatesMarksRollbackRequiredOnMismatch(t *testing.T) {
	d := &Daemon{
		cfg:           Config{WorkspacesRoot: t.TempDir()},
		agentVersions: map[string]string{"codex": "codex 0.138.0"},
	}
	record := providerCLIUpdateRecord{
		UpdateID:        "upd-1",
		Provider:        "codex",
		TargetVersion:   "0.139.0",
		RollbackVersion: "0.125.0",
		Status:          providerCLIUpdatePendingVerify,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := d.saveProviderCLIUpdateRecord(record); err != nil {
		t.Fatalf("save pending record: %v", err)
	}
	if err := d.verifyPendingProviderCLIUpdates(); err != nil {
		t.Fatalf("verify pending: %v", err)
	}
	records, err := d.loadProviderCLIUpdateRecords()
	if err != nil {
		t.Fatalf("load records: %v", err)
	}
	if got := records["upd-1"].Status; got != providerCLIUpdateRollbackRequired {
		t.Fatalf("status = %q, want rollback_required", got)
	}
}

func TestInProviderCLIUpdateWindowAllowsExplicitMidnight(t *testing.T) {
	d := &Daemon{cfg: Config{
		ProviderCLIUpdateWindowStart:           0,
		ProviderCLIUpdateWindowStartConfigured: true,
		ProviderCLIUpdateWindowDuration:        time.Hour,
	}}
	inside := time.Date(2026, 6, 12, 0, 30, 0, 0, time.UTC)
	outside := time.Date(2026, 6, 12, 4, 30, 0, 0, time.UTC)
	if !d.inProviderCLIUpdateWindow(inside) {
		t.Fatal("00:30 should be inside explicit 00:00 window")
	}
	if d.inProviderCLIUpdateWindow(outside) {
		t.Fatal("04:30 should be outside explicit 00:00 window")
	}
}
