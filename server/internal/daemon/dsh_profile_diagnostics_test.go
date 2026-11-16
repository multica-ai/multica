package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This admission diagnostic is emitted by the reported DSH 0.2.2-rc.3 tag.
// No actual DSH installation, credentials or model are used by these fixtures.
const dshCoreCompatibilityFixture = `dsh: disabling profile plugin row "agents": Plugin @deepseek-ai/dsh-agent@0.2.2-rc.1 is incompatible with dsh 0.2.2-rc.3: requirements {"engines.dsh":"0.2.2-rc.1"}.`

func TestProbeBuiltinRuntimeDshRetainsStartupDiagnosis(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI; classification tests are platform independent")
	}
	originalDelay, originalWindow := runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow
	t.Cleanup(func() { runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow = originalDelay, originalWindow })
	runtimeVersionProbeRetryDelay = time.Millisecond
	runtimeVersionProbeRetryWindow = 10 * time.Millisecond
	for _, tc := range []struct {
		name, stderr string
		want         builtinProbeVerdict
	}{
		{"confirmed core compatibility", dshCoreCompatibilityFixture, builtinProbeProfileMigration},
		{"unclassified startup failure", "permission denied opening profile cache", builtinProbeUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := dshProbeFixture(t, "exit 1", true)
			manifestPath := dshMulticaProfileManifestPath()
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			var manifest map[string]any
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest["dependencies"] = map[string]string{"@deepseek-ai/dsh-agent": "0.2.2-rc.1", "independent-required-hook": "1.2.3"}
			before, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, before, 0600); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "provisioned")
			t.Setenv(dshProfileBundleEnv, "@multica-ai/dsh-runtime")
			t.Setenv("DSH_DIAGNOSTIC_INSTALL_MARKER", marker)
			script := "#!/bin/sh\ncase \"$*\" in\n" +
				"*--version*) printf '%s\\n' '0.2.2-rc.3' ;;\n" +
				"*--probe*) printf '%s\\n' '" + tc.stderr + "' >&2; exit 1 ;;\n" +
				"plugin*) printf installed > \"$DSH_DIAGNOSTIC_INSTALL_MARKER\"; exit 1 ;;\n" +
				"*) exit 2 ;;\nesac\n"
			if err := os.WriteFile(path, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), agentVersions: map[string]string{}}
			probe := d.probeBuiltinRuntime(context.Background(), "dsh", AgentEntry{Path: path, Command: "dsh"})
			reason, verdict := probe.reason, probe.verdict
			if verdict != tc.want {
				t.Errorf("verdict=%v, want %v", verdict, tc.want)
			}
			for _, part := range []string{tc.stderr[:min(70, len(tc.stderr))], "0.2.2-rc.3", manifestPath} {
				if !strings.Contains(reason, part) {
					t.Errorf("startup reason lost %q: %q", part, reason)
				}
			}
			if tc.want == builtinProbeProfileMigration && !builtinProbeNeedsConfirmation(verdict) {
				t.Error("profile repair bypassed confirmation")
			}
			after, err := os.ReadFile(manifestPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("existing profile changed: %v", err)
			}
			time.Sleep(50 * time.Millisecond)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("provisioning ran for an existing profile: %v", err)
			}
		})
	}
}

func TestDshProfileDiagnosticClassification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI")
	}
	healthy := `{"v":1,"type":"probe","runtime":"dsh","protocol_version":1}`
	future := `{"v":2,"type":"probe","runtime":"dsh","protocol_version":2}`
	for _, tc := range []struct {
		name, stdout, stderr string
		code                 int
		hangs                bool
		want                 dshProbeVerdict
	}{
		{"core agent", "", dshCoreCompatibilityFixture, 1, false, dshProbeNeedsMigration},
		{"core session", "", strings.ReplaceAll(dshCoreCompatibilityFixture, "dsh-agent", "dsh-session"), 1, false, dshProbeNeedsMigration},
		{"core tools", "", strings.ReplaceAll(dshCoreCompatibilityFixture, "dsh-agent", "dsh-tools"), 1, false, dshProbeNeedsMigration},
		{"core llm", "", strings.ReplaceAll(dshCoreCompatibilityFixture, "dsh-agent", "dsh-llm"), 1, false, dshProbeNeedsMigration},
		{"core refusal exits zero", "", dshCoreCompatibilityFixture, 0, false, dshProbeNeedsMigration},
		{"optional plugin", "", strings.ReplaceAll(dshCoreCompatibilityFixture, "@deepseek-ai/dsh-agent", "@example/optional-plugin"), 1, false, dshProbeUnavailable},
		{"incidental incompatibility text", "", "Plugin @deepseek-ai/dsh-agent@0.2.2-rc.1 is incompatible with dsh 0.2.2-rc.3", 1, false, dshProbeUnavailable},
		{"healthy stdout wins", healthy, dshCoreCompatibilityFixture, 1, false, dshProbeOK},
		{"future stdout keeps protocol skew", future, dshCoreCompatibilityFixture, 1, false, dshProbeIncompatible},
		{"stderr cannot assert readiness", "", healthy, 1, false, dshProbeUnavailable},
		{"timeout cannot condemn", "", dshCoreCompatibilityFixture, 0, true, dshProbeUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.hangs {
				shortenDshProbeTimeoutForHang(t)
			}
			body := fmt.Sprintf("printf '%%s\\n' '%s'; printf '%%s\\n' '%s' >&2; ", tc.stdout, tc.stderr)
			if tc.hangs {
				body += "exec sleep 30"
			} else {
				body += fmt.Sprintf("exit %d", tc.code)
			}
			path := dshProbeFixture(t, body, true)
			if got := probeDshMulticaProfile(context.Background(), path).verdict; got != tc.want {
				t.Fatalf("profile verdict=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestProbeBuiltinRuntimeDshRedactsStartupDiagnosis(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI")
	}
	secret := "sk-" + strings.Repeat("synthetic", 5)
	password := "synthetic-registry-password"
	pemBody := strings.Repeat("SYNTHETIC", 20)
	stdout := "API_KEY=" + secret + "\n-----BEGIN PRIVATE KEY-----\n" + pemBody + "\n-----END PRIVATE KEY-----"
	stderr := "fatal probe startup: https://test:" + password + "@registry.example.test/package"
	body := fmt.Sprintf("printf '%%s\\n' '%s'; printf '%%s\\n' '%s' >&2; exit 1", stdout, stderr)
	path := dshProbeFixture(t, body, true)
	originalDelay, originalWindow := runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow
	t.Cleanup(func() { runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow = originalDelay, originalWindow })
	runtimeVersionProbeRetryDelay, runtimeVersionProbeRetryWindow = time.Millisecond, time.Millisecond
	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), agentVersions: map[string]string{}}
	probe := d.probeBuiltinRuntime(context.Background(), "dsh", AgentEntry{Path: path, Command: "dsh"})
	reason, verdict := probe.reason, probe.verdict
	if verdict != builtinProbeUnavailable || !strings.Contains(reason, "fatal probe startup") {
		t.Fatal("redacted startup diagnostic was lost")
	}
	for _, sensitive := range []string{secret, password, pemBody} {
		if strings.Contains(reason, sensitive) {
			t.Error("synthetic secret survived diagnostic redaction")
		}
	}
}

func TestDshCoreCompatibilitySummaryRedactsVersionTokens(t *testing.T) {
	secret := "sk-" + strings.Repeat("synthetic", 4)
	line := strings.Replace(dshCoreCompatibilityFixture, "0.2.2-rc.1", "0.2.2-"+secret, 1)
	summary := dshCoreCompatibilitySummary(line)
	if summary == "" {
		t.Fatal("core compatibility refusal was not recognized")
	}
	if strings.Contains(summary, secret) {
		t.Fatal("captured version token escaped diagnostic redaction")
	}
}

func TestDshCoreCompatibilityDemotionRequiresConfirmationAndRecovers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake CLI")
	}
	stubConfirmWindow(t, 0)
	path := dshProbeFixture(t, dshProbeOKFrame, true)
	manifestPath := dshMulticaProfileManifestPath()
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	fx := newBatchFixture(t)
	fx.enableStableRuntimeIDs()
	d := fx.daemon
	agents := map[string]AgentEntry{"dsh": {Path: path, Command: "dsh"}}
	d.cfg.Agents = agents
	stubAgentProbe(t, agents)
	fx.setProbeVersion("0.2.2-rc.3")
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	id := fx.runtimeIDFor("ws-1", "dsh")
	if id == "" || !fx.runtimeOnline(id) {
		t.Fatal("healthy fixture never registered")
	}
	broken := "#!/bin/sh\nprintf '%s\\n' '" + dshCoreCompatibilityFixture + "' >&2\nprintf 'unrelated-private-startup-text\\000\\n' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(broken), 0755); err != nil {
		t.Fatal(err)
	}
	d.refreshAgentVersions(context.Background())
	if !fx.runtimeOnline(id) {
		t.Fatal("first refusal demoted before confirmation")
	}
	d.refreshAgentVersions(context.Background())
	if fx.runtimeOnline(id) {
		t.Fatal("confirmed core refusal left the runtime online")
	}
	reason, ok := fx.runtimeOfflineReason(id)
	if !ok || reason.Code != RuntimeOfflineCodeDshProfile || reason.Installing || !strings.Contains(reason.Detail, "dsh-agent@0.2.2-rc.1") {
		t.Fatalf("offline cause lost the manual profile repair diagnosis: %+v", reason)
	}

	if strings.Contains(reason.Detail, "unrelated-private-startup-text") || strings.ContainsRune(reason.Detail, 0) {
		t.Error("durable runtime reason contains provider output or NUL")
	}
	if !strings.Contains(d.skippedAgentsSnapshot()["dsh"], "unrelated-private-startup-text") {
		t.Error("daemon diagnostic lost local startup output")
	}
	healthy := "#!/bin/sh\n" + dshProbeOKFrame + "\n"
	if err := os.WriteFile(path, []byte(healthy), 0755); err != nil {
		t.Fatal(err)
	}
	d.convergeRuntimeRegistrations(context.Background())
	if !fx.runtimeOnline(id) || fx.runtimeIDFor("ws-1", "dsh") != id {
		t.Fatal("healthy profile did not recover its existing runtime")
	}
	if _, ok := fx.runtimeOfflineReason(id); ok {
		t.Fatal("recovered runtime retained the stale repair reason")
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("probe lifecycle changed the user's profile: %v", err)
	}
}

func TestDshCoreCompatibilitySignalIsTransient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal fixture")
	}
	path := dshProbeFixture(t, "printf '%s\\n' '"+dshCoreCompatibilityFixture+"' >&2; kill -TERM $$", true)
	result := probeDshMulticaProfile(context.Background(), path)
	if result.verdict != dshProbeUnavailable {
		t.Fatalf("signal verdict=%v, want transient", result.verdict)
	}
	if !strings.Contains(result.detail, "dsh-agent@0.2.2-rc.1") {
		t.Fatal("signal lost available diagnostic")
	}
}
