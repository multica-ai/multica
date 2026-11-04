package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHermesInstallsReuseSource(t *testing.T) {
	t.Parallel()
	for _, conversation := range []bool{false, true} {
		mode := "task"
		if conversation {
			mode = "conversation"
		}
		for _, legacy := range []bool{false, true} {
			state := "fresh"
			if legacy {
				state = "legacy-link"
			}
			t.Run(mode+"/"+state, func(t *testing.T) {
				source := t.TempDir()
				record := filepath.Join("installs", "id", "pm-runtime", "selected.json")
				mustWrite(t, filepath.Join(source, record), `{"generation":"source"}`)
				taskHome := filepath.Join(t.TempDir(), "task")
				home := taskHome
				store := ""
				if conversation {
					store = filepath.Join(t.TempDir(), "conversation")
					home = store
				}
				if legacy {
					if err := os.MkdirAll(home, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := createDirLink(filepath.Join(source, "installs"), filepath.Join(home, "installs")); err != nil {
						t.Fatal(err)
					}
				}
				for turn := 0; turn < 2; turn++ {
					prepared, err := prepareHermesHome(taskHome, source, false, nil, nil, "", store, testLogger())
					if err != nil {
						t.Fatalf("prepare turn %d: %v", turn, err)
					}
					if got, err := os.ReadFile(filepath.Join(prepared.Home, record)); err != nil || string(got) != `{"generation":"source"}` {
						t.Fatalf("source dependency environment unavailable on turn %d: %q, %v", turn, got, err)
					}
					sourceInfo, err := os.Stat(filepath.Join(source, record))
					if err != nil {
						t.Fatal(err)
					}
					mirroredInfo, err := os.Stat(filepath.Join(prepared.Home, record))
					if err != nil || !os.SameFile(sourceInfo, mirroredInfo) {
						t.Fatalf("dependencies were copied instead of reused: %v", err)
					}
				}
				if got, err := os.ReadFile(filepath.Join(source, record)); err != nil || string(got) != `{"generation":"source"}` {
					t.Fatalf("source dependency environment changed: %q, %v", got, err)
				}
			})
		}
	}
}

func TestHermesInstallsRecoverDanglingLink(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"already-dangling", "source-removed"} {
		t.Run(scenario, func(t *testing.T) {
			source := t.TempDir()
			home := t.TempDir()
			installs := filepath.Join(home, "installs")
			if scenario == "source-removed" {
				mustWrite(t, filepath.Join(source, "installs", "record"), "SOURCE")
				if _, err := prepareHermesHome(home, source, false, nil, nil, "", "", testLogger()); err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(filepath.Join(source, "installs")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(filepath.Join(source, "installs"), installs); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if _, err := prepareHermesHome(home, source, false, nil, nil, "", "", testLogger()); err != nil {
				t.Fatalf("prepare with a stale dependency link: %v", err)
			}
			if _, err := os.Lstat(installs); !os.IsNotExist(err) {
				t.Fatalf("stale dependency link should be removed for PM initialization: %v", err)
			}
			mustWrite(t, filepath.Join(installs, "record"), "LOCAL REPAIR")
			if _, err := prepareHermesHome(home, source, false, nil, nil, "", "", testLogger()); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(filepath.Join(installs, "record")); err != nil || string(got) != "LOCAL REPAIR" {
				t.Fatalf("repaired dependencies were not retained: %q, %v", got, err)
			}
		})
	}
}

func TestHermesInstallsPreserveSharedLinkWithoutSource(t *testing.T) {
	t.Parallel()
	source, home, shared := t.TempDir(), t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(shared, "record"), "SHARED")
	installs := filepath.Join(home, "installs")
	if err := createDirLink(shared, installs); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(installs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareHermesHome(home, source, false, nil, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepare rejected a reusable dependency link: %v", err)
	}
	after, err := os.Lstat(installs)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("prepare replaced the dependency link: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(installs, "record")); err != nil || string(got) != "SHARED" {
		t.Fatalf("shared dependencies were not retained: %q, %v", got, err)
	}
}

func TestHermesInstallsPersistAcrossPrepare(t *testing.T) {
	t.Parallel()
	for _, conversation := range []bool{false, true} {
		mode := "task"
		if conversation {
			mode = "conversation"
		}
		for _, scenario := range []string{"same-source", "source-adds-installs", "source-switch", "source-switch-without-installs", "source-missing", "source-dangling-installs"} {
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
				if scenario != "source-switch-without-installs" {
					mustWrite(t, filepath.Join(otherSource, "installs", "source-record"), "OTHER INSTALL")
				}

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
				case "source-switch", "source-switch-without-installs":
					nextSource = otherSource
				case "source-missing":
					nextSource = filepath.Join(t.TempDir(), "missing")
				case "source-dangling-installs":
					requireSymlinks(t)
					if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(source, "installs")); err != nil {
						t.Fatal(err)
					}
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
				mirrored := scenario == "source-adds-installs" || scenario == "source-switch"
				if mirrored {
					want := "SOURCE INSTALL"
					if scenario == "source-switch" {
						want = "OTHER INSTALL"
					}
					if got, err := os.ReadFile(filepath.Join(installs, "source-record")); err != nil || string(got) != want {
						t.Fatalf("source dependencies were not mirrored: %q, %v", got, err)
					}
				} else if !after.IsDir() || !os.SameFile(before, after) {
					t.Fatal("prepare replaced the task-local installs directory")
				}
				for name, want := range local {
					if mirrored && strings.HasPrefix(name, "installs/") {
						continue // The source environment is authoritative when available.
					}
					got, err := os.ReadFile(filepath.Join(next.Home, name))
					if err != nil || string(got) != want {
						t.Errorf("task-local %s not preserved: %q, %v", name, got, err)
					}
				}
				if scenario == "source-switch" || scenario == "source-switch-without-installs" || scenario == "source-missing" {
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
				if scenario != "source-switch-without-installs" {
					otherInstall, err := os.ReadFile(filepath.Join(otherSource, "installs", "source-record"))
					if err != nil || string(otherInstall) != "OTHER INSTALL" {
						t.Errorf("other source PM state changed: %v", err)
					}
				}
				if scenario == "source-adds-installs" {
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
	for _, kind := range []string{"symlink-to-file", "file"} {
		t.Run(kind, func(t *testing.T) {
			source := t.TempDir()
			home := t.TempDir()
			installs := filepath.Join(home, "installs")
			target := filepath.Join(t.TempDir(), "shared-installs")
			if kind == "file" {
				mustWrite(t, installs, "KEEP FILE")
			} else {
				requireSymlinks(t)
				mustWrite(t, target, "KEEP SOURCE")
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
			if kind == "symlink-to-file" {
				if got, err := os.ReadFile(target); err != nil || string(got) != "KEEP SOURCE" {
					t.Errorf("shared target changed: %v", err)
				}
			}
		})
	}
}
