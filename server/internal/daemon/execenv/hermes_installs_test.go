package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHermesInstallsPersistAcrossPrepare(t *testing.T) {
	t.Parallel()
	for _, conversation := range []bool{false, true} {
		mode := "task"
		if conversation {
			mode = "conversation"
		}
		for _, scenario := range []string{"same-source", "source-adds-installs", "source-switch", "source-missing", "source-has-installs"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				source := t.TempDir()
				otherSource := t.TempDir()
				for _, home := range []string{source, otherSource} {
					mustWrite(t, filepath.Join(home, ".env"), "SYNTHETIC_KEY=fixture-only\n")
					mustWrite(t, filepath.Join(home, "state.db"), "SOURCE DB")
					mustWrite(t, filepath.Join(home, "gateway.lock"), "SOURCE GATEWAY")
				}
				mustWrite(t, filepath.Join(source, "old-auth.json"), "synthetic old credential")
				sourceInstall := filepath.Join(source, "installs", "source-record")
				if scenario == "source-has-installs" {
					mustWrite(t, sourceInstall, "SOURCE INSTALL")
				}
				mustWrite(t, filepath.Join(otherSource, "installs", "source-record"), "OTHER INSTALL")

				taskHome := filepath.Join(t.TempDir(), "task")
				store := ""
				if conversation {
					store = filepath.Join(t.TempDir(), "conversation")
				}
				first, err := prepareHermesHome(taskHome, source, false, nil, nil, "", store, testLogger())
				if err != nil {
					t.Fatal(err)
				}
				installs := filepath.Join(first.Home, "installs")
				if _, err := os.Lstat(installs); !os.IsNotExist(err) {
					t.Fatalf("first prepare should leave PM initialization local and lazy, stat installs: %v", err)
				}
				for _, name := range []string{"state.db", "gateway.lock"} {
					if _, err := os.Lstat(filepath.Join(first.Home, name)); !os.IsNotExist(err) {
						t.Fatalf("first prepare imported source %s: %v", name, err)
					}
				}

				local := map[string]string{
					"installs/synthetic-install/generations/g1/committed": "TASK GENERATION",
					"installs/synthetic-install/current.json":             "TASK PM RECORD",
					"state.db":     "TASK DB",
					"state.db-wal": "TASK WAL",
					"state.db-shm": "TASK SHM",
					"gateway.lock": "TASK GATEWAY",
				}
				for _, name := range []string{hermesTaskLocalStateMarker, hermesGatewayStateMarker} {
					content, err := os.ReadFile(filepath.Join(first.Home, name))
					if err != nil {
						t.Fatal(err)
					}
					local[name] = string(content)
				}
				for name, content := range local {
					mustWrite(t, filepath.Join(first.Home, name), content)
				}
				before, err := os.Lstat(installs)
				if err != nil {
					t.Fatal(err)
				}
				nextSource := source
				switch scenario {
				case "source-adds-installs":
					mustWrite(t, sourceInstall, "SOURCE INSTALL")
				case "source-switch":
					nextSource = otherSource
				case "source-missing":
					nextSource = filepath.Join(t.TempDir(), "missing")
				}
				if conversation {
					taskHome = filepath.Join(t.TempDir(), "next-task")
				}
				next, err := prepareHermesHome(taskHome, nextSource, false, nil, nil, "", store, testLogger())
				if err != nil {
					t.Fatal(err)
				}
				if next.Home != first.Home {
					t.Fatalf("effective home changed: %q != %q", next.Home, first.Home)
				}
				after, err := os.Lstat(installs)
				if err != nil {
					t.Fatalf("prepare removed committed task-local installs: %v", err)
				}
				if !after.IsDir() || !os.SameFile(before, after) {
					t.Fatal("prepare replaced the task-local installs directory")
				}
				for name, want := range local {
					got, err := os.ReadFile(filepath.Join(next.Home, name))
					if err != nil || string(got) != want {
						t.Errorf("task-local %s not preserved: %q, %v", name, got, err)
					}
				}
				if scenario == "source-switch" || scenario == "source-missing" {
					if _, err := os.Lstat(filepath.Join(next.Home, "old-auth.json")); !os.IsNotExist(err) {
						t.Errorf("stale source credential survived reconciliation: %v", err)
					}
				}
				derivedEnv, err := os.ReadFile(filepath.Join(next.Home, ".env"))
				if err != nil || !strings.Contains(string(derivedEnv), "HERMES_HOME='"+next.Home+"'") {
					t.Errorf("derived .env did not pin isolated home: %v", err)
				}
				if scenario == "source-missing" && strings.Contains(string(derivedEnv), "SYNTHETIC_KEY") {
					t.Error("stale dotenv credential survived source removal")
				}
				for _, home := range []string{source, otherSource} {
					for name, want := range map[string]string{"state.db": "SOURCE DB", "gateway.lock": "SOURCE GATEWAY", ".env": "SYNTHETIC_KEY=fixture-only\n"} {
						got, err := os.ReadFile(filepath.Join(home, name))
						if err != nil || string(got) != want {
							t.Errorf("source %s changed: %v", name, err)
						}
					}
				}
				otherInstall, err := os.ReadFile(filepath.Join(otherSource, "installs", "source-record"))
				if err != nil || string(otherInstall) != "OTHER INSTALL" {
					t.Errorf("other source PM state changed: %v", err)
				}
				if scenario == "source-adds-installs" || scenario == "source-has-installs" {
					got, err := os.ReadFile(sourceInstall)
					if err != nil || string(got) != "SOURCE INSTALL" {
						t.Errorf("source PM state changed: %v", err)
					}
				}
			})
		}
	}
}

func TestHermesInstallsRejectsUnsafeEntries(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"symlink", "dangling-symlink", "file"} {
		t.Run(kind, func(t *testing.T) {
			source := t.TempDir()
			home := t.TempDir()
			installs := filepath.Join(home, "installs")
			target := filepath.Join(t.TempDir(), "shared-installs")
			if kind == "file" {
				mustWrite(t, installs, "KEEP FILE")
			} else {
				requireSymlinks(t)
				if kind == "symlink" {
					mustWrite(t, filepath.Join(target, "record"), "KEEP SOURCE")
				}
				if err := os.Symlink(target, installs); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := prepareHermesHome(home, source, false, nil, nil, "", "", testLogger()); err == nil || !strings.Contains(err.Error(), "installs") {
				t.Fatalf("expected explicit installs validation error, got %v", err)
			}
			if kind == "file" {
				if got, err := os.ReadFile(installs); err != nil || string(got) != "KEEP FILE" {
					t.Errorf("invalid entry changed: %v", err)
				}
			} else if got, err := os.Readlink(installs); err != nil || got != target {
				t.Errorf("unsafe link changed: %v", err)
			}
			if kind == "symlink" {
				if got, err := os.ReadFile(filepath.Join(target, "record")); err != nil || string(got) != "KEEP SOURCE" {
					t.Errorf("shared target changed: %v", err)
				}
			}
		})
	}
}
