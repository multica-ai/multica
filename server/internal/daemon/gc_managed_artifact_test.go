package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// The managed Codex cache (codex-home/.sandbox-bin, a ~285 MiB copy of the
// Codex binary) used to be reclaimed only on the issue decision path. Every
// other kind held it for as long as its parent record said "keep the
// directory" — unbounded for a chat session, which stays "active" with no time
// limit. See #6782, and #5654 for the original issue-path fix.
//
// sandboxBinRel is the exact managed subpath, spelled out here so a change to
// ManagedReclaimableArtifactSubpaths has to face these tests.
const sandboxBinRel = "codex-home/.sandbox-bin"

// chatGCMux serves a chat gc-check that reports the given status.
func chatGCMux(chatID, status string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/chat-sessions/%s/gc-check", chatID), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     status,
			"updated_at": time.Now().Add(-100 * 24 * time.Hour),
		})
	})
	return mux
}

func assertGone(t *testing.T, taskDir, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(taskDir, rel)); !os.IsNotExist(err) {
		t.Fatalf("%s should have been reclaimed, stat err = %v", rel, err)
	}
}

func assertKept(t *testing.T, taskDir string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		if _, err := os.Stat(filepath.Join(taskDir, rel)); err != nil {
			t.Fatalf("%s must be preserved: %v", rel, err)
		}
	}
}

// TestManagedArtifact_IdleActiveChatReclaimsSandboxBin is the #6782 regression
// test: an active chat session idle past GCArtifactTTL gives the managed Codex
// cache back, while everything the session owns survives.
func TestManagedArtifact_IdleActiveChatReclaimsSandboxBin(t *testing.T) {
	t.Parallel()
	chatID := "aaaaaaaa-0000-0000-0000-000000000001"
	d := newGCTestDaemon(t, chatGCMux(chatID, "active"))

	meta := &execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: chatID,
		WorkspaceID:   "ws",
		CompletedAt:   time.Now().Add(-24 * time.Hour), // 2x the 12h artifact TTL
	}
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "idle-chat", meta)
	writeFile(t, filepath.Join(taskDir, sandboxBinRel, "codex"), 4096)
	// Everything below must survive: session data, the audit trail, and a
	// user-owned directory that merely shares the managed basename.
	writeFile(t, filepath.Join(taskDir, "logs/run.log"), 32)
	writeFile(t, filepath.Join(taskDir, "output/result.md"), 32)
	writeFile(t, filepath.Join(taskDir, "codex-home/auth.json"), 32)
	writeFile(t, filepath.Join(taskDir, "workdir/repo/.sandbox-bin/user-owned"), 32)

	stats := &gcStats{byPattern: map[string]int{}}
	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionCleanManagedArtifacts {
		t.Fatalf("want gcActionCleanManagedArtifacts, got %d", action)
	}
	d.applyGCAction(taskDir, action, stats)

	assertGone(t, taskDir, sandboxBinRel)
	assertKept(t, taskDir,
		"logs/run.log",
		"output/result.md",
		"codex-home/auth.json",
		"workdir/repo/.sandbox-bin/user-owned",
		".gc_meta.json",
	)
	if stats.artifactRemoved != 1 {
		t.Fatalf("artifact_removed = %d, want 1", stats.artifactRemoved)
	}
	if got := stats.byPattern[managedArtifactPatternPrefix+sandboxBinRel]; got != 1 {
		t.Fatalf("managed pattern count = %d, want 1", got)
	}
}

// A chat session that completed recently keeps its cache — resuming inside the
// TTL must not pay for a re-provision.
func TestManagedArtifact_FreshActiveChatKeepsSandboxBin(t *testing.T) {
	t.Parallel()
	chatID := "aaaaaaaa-0000-0000-0000-000000000002"
	d := newGCTestDaemon(t, chatGCMux(chatID, "active"))

	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "fresh-chat", &execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: chatID,
		WorkspaceID:   "ws",
		CompletedAt:   time.Now().Add(-1 * time.Hour), // well inside the 12h TTL
	})
	writeFile(t, filepath.Join(taskDir, sandboxBinRel, "codex"), 4096)

	if got := d.shouldCleanTaskDir(context.Background(), taskDir); got != gcActionSkip {
		t.Fatalf("want gcActionSkip inside the TTL, got %d", got)
	}
	assertKept(t, taskDir, sandboxBinRel+"/codex")
}

// MULTICA_GC_ARTIFACT_TTL=0 disables the managed reclaim along with the rest of
// artifact cleanup — the documented opt-out.
func TestManagedArtifact_ArtifactTTLZeroDisablesFallback(t *testing.T) {
	t.Parallel()
	chatID := "aaaaaaaa-0000-0000-0000-000000000003"
	d := newGCTestDaemon(t, chatGCMux(chatID, "active"))
	d.cfg.GCArtifactTTL = 0

	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "no-ttl", &execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: chatID,
		WorkspaceID:   "ws",
		CompletedAt:   time.Now().Add(-365 * 24 * time.Hour),
	})
	writeFile(t, filepath.Join(taskDir, sandboxBinRel, "codex"), 4096)

	if got := d.shouldCleanTaskDir(context.Background(), taskDir); got != gcActionSkip {
		t.Fatalf("want gcActionSkip with artifact TTL disabled, got %d", got)
	}
	assertKept(t, taskDir, sandboxBinRel+"/codex")
}

// Metadata with no completed_at stays with the per-kind legacy handling rather
// than being reclaimed off an unrelated clock.
func TestManagedArtifact_ZeroCompletedAtIsLeftAlone(t *testing.T) {
	t.Parallel()
	chatID := "aaaaaaaa-0000-0000-0000-000000000004"
	d := newGCTestDaemon(t, chatGCMux(chatID, "active"))

	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "legacy-chat", &execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: chatID,
		WorkspaceID:   "ws",
	})
	writeFile(t, filepath.Join(taskDir, sandboxBinRel, "codex"), 4096)

	if got := d.shouldCleanTaskDir(context.Background(), taskDir); got != gcActionSkip {
		t.Fatalf("want gcActionSkip for zero completed_at, got %d", got)
	}
	assertKept(t, taskDir, sandboxBinRel+"/codex")
}

// The fallback is a one-way upgrade from skip: it must never weaken a decision
// that already removes more.
func TestManagedArtifact_NeverDowngradesStrongerActions(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())
	meta := &execenv.GCMeta{
		Kind:        execenv.GCKindChat,
		WorkspaceID: "ws",
		CompletedAt: time.Now().Add(-365 * 24 * time.Hour),
	}

	for _, action := range []gcAction{gcActionClean, gcActionOrphan, gcActionCleanArtifacts, gcActionCleanManagedArtifacts} {
		if got := d.applyManagedArtifactFallback("dir", meta, action); got != action {
			t.Fatalf("action %d was rewritten to %d", action, got)
		}
	}
}

// The same hole existed for the other non-issue kinds. These reach a terminal
// state quickly in practice, so they are bounded either way — but a run that
// sits non-terminal should not pin the cache.
func TestManagedArtifact_NonTerminalAutopilotAndQuickCreate(t *testing.T) {
	t.Parallel()
	runID := "bbbbbbbb-0000-0000-0000-000000000001"
	taskID := "bbbbbbbb-0000-0000-0000-000000000002"

	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/daemon/autopilot-runs/%s/gc-check", runID), func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "running"})
	})
	mux.HandleFunc(fmt.Sprintf("/api/daemon/tasks/%s/gc-check", taskID), func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "running"})
	})
	d := newGCTestDaemon(t, mux)

	cases := []struct {
		name string
		meta *execenv.GCMeta
	}{
		{"autopilot_run", &execenv.GCMeta{Kind: execenv.GCKindAutopilotRun, AutopilotRunID: runID, WorkspaceID: "ws"}},
		{"quick_create", &execenv.GCMeta{Kind: execenv.GCKindQuickCreate, TaskID: taskID, WorkspaceID: "ws"}},
	}
	for _, tc := range cases {
		tc.meta.CompletedAt = time.Now().Add(-24 * time.Hour)
		taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", tc.name, tc.meta)
		writeFile(t, filepath.Join(taskDir, sandboxBinRel, "codex"), 4096)

		action := d.shouldCleanTaskDir(context.Background(), taskDir)
		if action != gcActionCleanManagedArtifacts {
			t.Fatalf("%s: want gcActionCleanManagedArtifacts, got %d", tc.name, action)
		}
		d.applyGCAction(taskDir, action, &gcStats{byPattern: map[string]int{}})
		assertGone(t, taskDir, sandboxBinRel)
		assertKept(t, taskDir, ".gc_meta.json")
	}
}

// A task actively running on the directory keeps its cache — the fallback sits
// behind the isActiveEnvRoot short-circuit.
func TestManagedArtifact_ActiveEnvRootKeepsSandboxBin(t *testing.T) {
	t.Parallel()
	chatID := "aaaaaaaa-0000-0000-0000-000000000005"
	d := newGCTestDaemon(t, chatGCMux(chatID, "active"))

	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "running-chat", &execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: chatID,
		WorkspaceID:   "ws",
		CompletedAt:   time.Now().Add(-365 * 24 * time.Hour),
	})
	writeFile(t, filepath.Join(taskDir, sandboxBinRel, "codex"), 4096)

	d.markActiveEnvRoot(taskDir)
	if got := d.shouldCleanTaskDir(context.Background(), taskDir); got != gcActionSkip {
		t.Fatalf("want gcActionSkip for an active env root, got %d", got)
	}
	assertKept(t, taskDir, sandboxBinRel+"/codex")
}

// local_directory tasks keep their envRoot forever, so the managed cache is the
// only thing the GC may ever take from them. The fallback must still apply.
func TestManagedArtifact_LocalDirectoryChatReclaimsSandboxBin(t *testing.T) {
	t.Parallel()
	chatID := "aaaaaaaa-0000-0000-0000-000000000006"
	d := newGCTestDaemon(t, chatGCMux(chatID, "active"))

	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "local-chat", &execenv.GCMeta{
		Kind:           execenv.GCKindChat,
		ChatSessionID:  chatID,
		WorkspaceID:    "ws",
		LocalDirectory: true,
		CompletedAt:    time.Now().Add(-24 * time.Hour),
	})
	writeFile(t, filepath.Join(taskDir, sandboxBinRel, "codex"), 4096)
	writeFile(t, filepath.Join(taskDir, "logs/run.log"), 32)

	action := d.shouldCleanTaskDir(context.Background(), taskDir)
	if action != gcActionCleanManagedArtifacts {
		t.Fatalf("want gcActionCleanManagedArtifacts, got %d", action)
	}
	d.applyGCAction(taskDir, action, &gcStats{byPattern: map[string]int{}})
	assertGone(t, taskDir, sandboxBinRel)
	assertKept(t, taskDir, "logs/run.log", ".gc_meta.json")
}

// Once the cache is gone, a long-lived active chat must stop re-deciding to
// reclaim it. completed_at never moves again for a session that stays active,
// so without a presence check the GC would take an env-root reservation and run
// a removal pass every cycle, forever, for a directory with nothing to give.
func TestManagedArtifact_SecondCycleIsANoOp(t *testing.T) {
	t.Parallel()
	chatID := "aaaaaaaa-0000-0000-0000-000000000007"
	d := newGCTestDaemon(t, chatGCMux(chatID, "active"))

	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "repeat-chat", &execenv.GCMeta{
		Kind:          execenv.GCKindChat,
		ChatSessionID: chatID,
		WorkspaceID:   "ws",
		CompletedAt:   time.Now().Add(-24 * time.Hour),
	})
	writeFile(t, filepath.Join(taskDir, sandboxBinRel, "codex"), 4096)

	first := d.shouldCleanTaskDir(context.Background(), taskDir)
	if first != gcActionCleanManagedArtifacts {
		t.Fatalf("first cycle: want gcActionCleanManagedArtifacts, got %d", first)
	}
	d.applyGCAction(taskDir, first, &gcStats{byPattern: map[string]int{}})
	assertGone(t, taskDir, sandboxBinRel)

	for cycle := 2; cycle <= 3; cycle++ {
		if got := d.shouldCleanTaskDir(context.Background(), taskDir); got != gcActionSkip {
			t.Fatalf("cycle %d: want gcActionSkip once the cache is gone, got %d", cycle, got)
		}
	}

	// A later Codex run re-provisions the cache; the GC must notice it again.
	writeFile(t, filepath.Join(taskDir, sandboxBinRel, "codex"), 4096)
	if got := d.shouldCleanTaskDir(context.Background(), taskDir); got != gcActionCleanManagedArtifacts {
		t.Fatalf("want reclaim after re-provision, got %d", got)
	}
}

// The direct-path removal replaced a tree walk that refused to descend through
// symlinks and junctions. It has to refuse at every component, not just the
// leaf — codex-home links the user's real ~/.codex content into itself.
func TestManagedArtifact_DirectRemovalDoesNotFollowLinks(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())

	for _, tc := range []struct {
		name     string
		linkPath string
	}{
		{name: "leaf", linkPath: sandboxBinRel},
		{name: "parent", linkPath: "codex-home"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskDir := t.TempDir()
			outside := t.TempDir()
			keepFile := filepath.Join(outside, "keep")
			writeFile(t, keepFile, 10)
			// Mirror the shape that makes this dangerous: the link target is
			// the user's real ~/.codex, which has a .sandbox-bin of its own.
			// Traversing the link would resolve the managed path onto it.
			userSandboxBin := filepath.Join(outside, ".sandbox-bin", "user-owned")
			writeFile(t, userSandboxBin, 10)

			linkPath := filepath.Join(taskDir, filepath.FromSlash(tc.linkPath))
			if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, linkPath); err != nil {
				t.Skipf("symlink not supported: %v", err)
			}

			removed, bytes, _ := d.cleanManagedTaskArtifacts(taskDir)
			if removed != 0 || bytes != 0 {
				t.Fatalf("removed=%d bytes=%d, want 0 through a link", removed, bytes)
			}
			for _, keep := range []string{keepFile, userSandboxBin} {
				if _, err := os.Stat(keep); err != nil {
					t.Fatalf("link target was touched: %v", err)
				}
			}
		})
	}
}

// A repository directory that merely shares the managed leaf name is not the
// managed path and must survive the direct removal.
func TestManagedArtifact_DirectRemovalIgnoresSameNamedUserDir(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())

	taskDir := t.TempDir()
	writeFile(t, filepath.Join(taskDir, "workdir/repo/.sandbox-bin/user-owned"), 32)
	writeFile(t, filepath.Join(taskDir, "codex-home/auth.json"), 32)

	if removed, _, _ := d.cleanManagedTaskArtifacts(taskDir); removed != 0 {
		t.Fatalf("removed=%d, want 0 when the managed path is absent", removed)
	}
	assertKept(t, taskDir, "workdir/repo/.sandbox-bin/user-owned", "codex-home/auth.json")
}

// When the batch issue check cannot answer (server error, scoped token), the
// task data stays but the regenerable cache is still reclaimed.
func TestManagedArtifact_UnreachableIssueStillReclaimsCache(t *testing.T) {
	t.Parallel()
	issueID := "cccccccc-0000-0000-0000-000000000001"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/daemon/workspaces/ws/issues/gc-checks", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	mux.HandleFunc(fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	d := newGCTestDaemon(t, mux)

	meta := &execenv.GCMeta{
		Kind:        execenv.GCKindIssue,
		IssueID:     issueID,
		WorkspaceID: "ws",
		CompletedAt: time.Now().Add(-24 * time.Hour),
	}
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "unreachable-issue", meta)
	writeFile(t, filepath.Join(taskDir, sandboxBinRel, "codex"), 4096)
	writeFile(t, filepath.Join(taskDir, "output/result.md"), 32)

	stats := &gcStats{byPattern: map[string]int{}}
	d.gcWorkspaceIssues(context.Background(), "ws", []issueGCCandidate{{taskDir: taskDir, meta: meta}}, stats)

	assertGone(t, taskDir, sandboxBinRel)
	assertKept(t, taskDir, "output/result.md", ".gc_meta.json")
}

// The managed reclaim used to share cleanTaskArtifacts' tree walk and now
// addresses its paths directly. The two must agree on exactly what they remove
// — this pins that equivalence against the walk, which is still live for the
// basename patterns, so a future managed subpath or a change to either side
// cannot silently drift them apart.
func TestManagedArtifact_DirectRemovalMatchesTreeWalk(t *testing.T) {
	t.Parallel()

	fixtures := map[string]func(t *testing.T, dir string){
		"managed present": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, sandboxBinRel, "codex"), 4096)
			writeFile(t, filepath.Join(dir, "codex-home/auth.json"), 16)
		},
		"managed absent": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "codex-home/auth.json"), 16)
		},
		"user-owned same name only": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "workdir/repo/.sandbox-bin/keep"), 16)
		},
		"both managed and user-owned": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, sandboxBinRel, "codex"), 4096)
			writeFile(t, filepath.Join(dir, "workdir/repo/.sandbox-bin/keep"), 16)
		},
		"nested content and .git": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, sandboxBinRel, "a/b/c/codex"), 2048)
			writeFile(t, filepath.Join(dir, sandboxBinRel, ".git/objects/x"), 16)
			writeFile(t, filepath.Join(dir, "workdir/repo/.git/objects/y"), 16)
		},
		"codex-home is a regular file": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "codex-home"), 16)
		},
		"sandbox-bin is a regular file": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, sandboxBinRel), 16)
		},
		"leaf symlink": func(t *testing.T, dir string) {
			outside := t.TempDir()
			writeFile(t, filepath.Join(outside, ".sandbox-bin/x"), 16)
			if err := os.MkdirAll(filepath.Join(dir, "codex-home"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, sandboxBinRel)); err != nil {
				t.Skipf("symlink not supported: %v", err)
			}
		},
		"parent symlink": func(t *testing.T, dir string) {
			outside := t.TempDir()
			writeFile(t, filepath.Join(outside, ".sandbox-bin/x"), 16)
			if err := os.Symlink(outside, filepath.Join(dir, "codex-home")); err != nil {
				t.Skipf("symlink not supported: %v", err)
			}
		},
		"empty task dir": func(t *testing.T, dir string) {},
	}

	d := newGCTestDaemon(t, http.NewServeMux())
	walkMatcher := newArtifactMatcher(nil, execenv.ManagedReclaimableArtifactSubpaths())

	for name, setup := range fixtures {
		t.Run(name, func(t *testing.T) {
			walked, direct := t.TempDir(), t.TempDir()
			setup(t, walked)
			setup(t, direct)

			wRemoved, wBytes, wPattern := d.cleanTaskArtifactsMatching(walked, walkMatcher)
			dRemoved, dBytes, dPattern := d.cleanManagedTaskArtifacts(direct)

			if wRemoved != dRemoved || wBytes != dBytes || fmt.Sprint(wPattern) != fmt.Sprint(dPattern) {
				t.Fatalf("walk=(%d,%d,%v) direct=(%d,%d,%v)", wRemoved, wBytes, wPattern, dRemoved, dBytes, dPattern)
			}
			if w, dd := survivingTree(t, walked), survivingTree(t, direct); fmt.Sprint(w) != fmt.Sprint(dd) {
				t.Fatalf("survivors differ:\n walk=%v\n direct=%v", w, dd)
			}
		})
	}
}

// survivingTree lists every path left under root, relative to it, without
// following links out of the tree.
func survivingTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		out = append(out, rel)
		if e.Type()&os.ModeSymlink != 0 {
			return filepath.SkipDir
		}
		return nil
	})
	return out
}
