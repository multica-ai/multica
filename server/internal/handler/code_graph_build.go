package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/daemon/processtree"
	"github.com/multica-ai/multica/server/pkg/codegraph"
)

func codeGraphBuildDir(ws, repo, build string) string {
	return filepath.Join(codeGraphCheckoutDir(ws, repo), "builds", build, "repo")
}

func codeGraphWorkDir(ws, repo, build string) string {
	root := os.Getenv("MULTICA_CODEGRAPH_WORK_ROOT")
	if root == "" {
		root = filepath.Join(os.TempDir(), "multica-codegraph-builds")
	}
	return filepath.Join(root, ws, repo, "builds", build, "repo")
}

// The unique partial index is the final arbiter for concurrent enqueue requests.
func (h *Handler) queueCodeGraph(ctx context.Context, repo codeGraphRepositoryRow) error {
	if os.Getenv("MULTICA_CODEGRAPH_ENGINE_URL") == "" {
		err := errors.New("Configure MULTICA_CODEGRAPH_ENGINE_URL before building graphs")
		h.markCodeGraphFailed(ctx, repo, "engine_unconfigured", err)
		return err
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var lockedID pgtype.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM code_graph_repository WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, repo.ID, repo.WorkspaceID).Scan(&lockedID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO code_graph_build(workspace_id,repository_id,status,phase) VALUES($1,$2,'queued','queued') ON CONFLICT (repository_id) WHERE status IN ('queued','processing') DO NOTHING`, repo.WorkspaceID, repo.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE code_graph_repository SET status=CASE WHEN EXISTS(SELECT 1 FROM code_graph_build WHERE repository_id=$1 AND status='processing') THEN 'processing' ELSE 'pending' END, internal_status='queued',sync_error=NULL,updated_at=now() WHERE id=$1`, repo.ID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// One session lock owns the persistent queue across server replicas. A replacement
// worker marks interrupted builds failed; it never silently repeats unknown work.
func (h *Handler) RunCodeGraphBuilds(ctx context.Context, pool *pgxpool.Pool) {
	if os.Getenv("MULTICA_CODEGRAPH_ENGINE_URL") == "" {
		return
	}
	for ctx.Err() == nil {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return
		}
		var locked bool
		err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(7931123807)`).Scan(&locked)
		if err == nil && locked {
			h.runCodeGraphQueue(ctx, conn)
			// Never return a session-scoped lock to the pool, even during shutdown.
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = conn.Conn().Close(closeCtx)
			cancel()
		}
		conn.Release()
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (h *Handler) runCodeGraphQueue(ctx context.Context, conn *pgxpool.Conn) {
	rows, err := conn.Query(ctx, `SELECT id,workspace_id,repository_id FROM code_graph_build WHERE status='processing'`)
	if err != nil {
		return
	}
	type interruptedBuild struct{ id, workspace, repository pgtype.UUID }
	var interrupted []interruptedBuild
	for rows.Next() {
		var entry interruptedBuild
		if rows.Scan(&entry.id, &entry.workspace, &entry.repository) != nil {
			rows.Close()
			return
		}
		interrupted = append(interrupted, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	for _, entry := range interrupted {
		if h.stopCodeGraphEngine(ctx, entry.id) != nil {
			return
		}
		cleanupCtx, cancelCleanup := context.WithTimeout(ctx, time.Minute)
		_, cleanupErr := h.codeGraphEngine(cleanupCtx, map[string]any{"op": "cleanup", "workspace_id": uuidToString(entry.workspace), "repository_id": uuidToString(entry.repository), "build_id": uuidToString(entry.id)})
		cancelCleanup()
		if cleanupErr != nil {
			slog.Warn("code graph interrupted cleanup failed", "build_id", uuidToString(entry.id))
		}
	}
	_, err = conn.Exec(ctx, `WITH interrupted AS (UPDATE code_graph_build SET status='failed',phase=CASE WHEN cancel_requested THEN 'cancelled' ELSE 'interrupted' END,error=CASE WHEN cancel_requested THEN 'Build cancelled by user' ELSE 'Worker restarted; rebuild explicitly.' END,finished_at=now() WHERE status='processing' RETURNING repository_id,phase,error) UPDATE code_graph_repository r SET status='failed',internal_status=i.phase,sync_error=i.error,updated_at=now() FROM interrupted i WHERE r.id=i.repository_id`)
	if err != nil {
		slog.Error("code graph queue recovery failed", "error", err)
		return
	}
	ctx, cancelQueue := context.WithCancel(ctx)
	var workers sync.WaitGroup
	pipeline := codegraph.NewPipeline()
	defer func() { cancelQueue(); workers.Wait() }()
	for ctx.Err() == nil {
		select {
		case pipeline.Builds <- struct{}{}:
		case <-ctx.Done():
			return
		}
		var build, repo pgtype.UUID
		err = conn.QueryRow(ctx, `UPDATE code_graph_build SET status='processing',started_at=now() WHERE id=(SELECT id FROM code_graph_build WHERE status='queued' ORDER BY created_at LIMIT 1) RETURNING id,repository_id`).Scan(&build, &repo)
		if errors.Is(err, pgx.ErrNoRows) {
			<-pipeline.Builds
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		if err != nil {
			<-pipeline.Builds
			slog.Error("code graph queue claim failed", "error", err)
			return
		}
		row, loadErr := scanCodeGraphRepository(conn.QueryRow(ctx, `SELECT `+codeGraphSelectColumns()+` FROM code_graph_repository WHERE id=$1`, repo))
		if loadErr != nil {
			_, _ = conn.Exec(ctx, `UPDATE code_graph_build SET status='failed',error='Repository removed',finished_at=now() WHERE id=$1`, build)
			<-pipeline.Builds
			continue
		}
		workers.Add(1)
		go func(row codeGraphRepositoryRow, build, repo pgtype.UUID) {
			defer workers.Done()
			defer func() { <-pipeline.Builds }()
			defer func() {
				if ctx.Err() != nil {
					return
				}
				var unfinished bool
				checkErr := h.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM code_graph_build WHERE id=$1 AND status='processing')`, build).Scan(&unfinished)
				if checkErr != nil || unfinished {
					// Recover under a fresh dispatcher only after all workers are drained.
					cancelQueue()
				}
			}()
			buildCtx, cancel := context.WithCancel(ctx)
			watchCtx, stopWatch := context.WithCancel(buildCtx)
			watchDone := make(chan struct{})
			go func() {
				defer close(watchDone)
				ticker := time.NewTicker(3 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-watchCtx.Done():
						return
					case <-ticker.C:
						var exists bool
						if err := h.DB.QueryRow(watchCtx, `SELECT EXISTS(SELECT 1 FROM code_graph_build WHERE id=$1 AND status='processing' AND NOT cancel_requested)`, build).Scan(&exists); err != nil {
							slog.Warn("code graph cancellation check failed", "build_id", uuidToString(build))
						} else if !exists {
							cancel()
							return
						}
					}
				}
			}()
			err := h.buildCodeGraph(buildCtx, ctx, row, build, pipeline)
			stopWatch()
			<-watchDone
			cancel()
			if err != nil {
				if strings.Contains(err.Error(), "stop process tree:") {
					// Do not permit deletion while an OS process may still own the checkout.
					_, _ = h.DB.Exec(ctx, `UPDATE code_graph_repository SET internal_status='cancelling',sync_error='Process termination could not be confirmed; build remains locked. Check runtime storage and process health.',updated_at=now() WHERE id=$1`, repo)
					<-ctx.Done()
					return
				}
				// A disconnected HTTP request is not an acknowledgement of engine exit.
				for ctx.Err() == nil {
					if h.stopCodeGraphEngine(ctx, build) == nil {
						break
					}
					slog.Warn("code graph stop awaiting engine acknowledgement", "build_id", uuidToString(build))
					select {
					case <-ctx.Done():
						return
					case <-time.After(3 * time.Second):
					}
				}
				if ctx.Err() != nil {
					return
				}
				msg := err.Error()
				phase := "build_failed"
				if len(msg) > 1000 {
					msg = msg[:1000]
				}
				// Serialize finalization with enqueue, cancellation and deletion.
				tx, txErr := h.TxStarter.Begin(ctx)
				if txErr != nil {
					return
				}
				_, txErr = tx.Exec(ctx, `SELECT id FROM code_graph_repository WHERE id=$1 FOR UPDATE`, repo)
				var cancelled bool
				if txErr == nil {
					txErr = tx.QueryRow(ctx, `SELECT cancel_requested,phase FROM code_graph_build WHERE id=$1 FOR UPDATE`, build).Scan(&cancelled, &phase)
				}
				if cancelled {
					msg = "Build cancelled by user"
					phase = "cancelled"
				}
				if txErr == nil {
					_, txErr = tx.Exec(ctx, `UPDATE code_graph_build SET status='failed',phase=$3,error=$2,finished_at=now() WHERE id=$1 AND status='processing'`, build, msg, phase)
				}
				if txErr == nil {
					_, txErr = tx.Exec(ctx, `UPDATE code_graph_repository SET status='failed',internal_status=$2,sync_error=$3,updated_at=now() WHERE id=$1`, repo, phase, msg)
				}
				if txErr != nil {
					_ = tx.Rollback(ctx)
					return
				}
				if tx.Commit(ctx) != nil {
					return
				}
			}
			// Only clean after the build has finished or engine termination is acknowledged.
			cleanupCtx, cleanupCancel := context.WithTimeout(ctx, time.Minute)
			if _, cleanupErr := h.codeGraphEngine(cleanupCtx, map[string]any{"op": "cleanup", "workspace_id": uuidToString(row.WorkspaceID), "repository_id": uuidToString(row.ID), "build_id": uuidToString(build)}); cleanupErr != nil {
				slog.Warn("code graph temporary cleanup failed", "build_id", uuidToString(build))
			}
			cleanupCancel()
		}(row, build, repo)
	}
}

func (h *Handler) stopCodeGraphEngine(ctx context.Context, build pgtype.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"build_id": uuidToString(build)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(os.Getenv("MULTICA_CODEGRAPH_ENGINE_URL"), "/")+"/cancel", bytes.NewReader(body))
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		return errors.New("Engine has not confirmed stop")
	}
	return nil
}

func (h *Handler) codeGraphPhase(ctx context.Context, repo, build pgtype.UUID, phase string) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM code_graph_repository WHERE id=$1 FOR UPDATE`, repo); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE code_graph_build SET phase=$2,phase_started_at=now() WHERE id=$1 AND status='processing' AND NOT cancel_requested`, build, phase)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return context.Canceled
	}
	if _, err = tx.Exec(ctx, `UPDATE code_graph_repository SET status='processing',internal_status=$2,updated_at=now() WHERE id=$1`, repo, phase); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (h *Handler) codeGraphEngine(ctx context.Context, input map[string]any) (json.RawMessage, error) {
	endpoint := strings.TrimRight(os.Getenv("MULTICA_CODEGRAPH_ENGINE_URL"), "/")
	if endpoint == "" {
		return nil, errors.New("CodeGraph engine is not configured")
	}
	body, _ := json.Marshal(input)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/query", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: codegraph.BuildTimeout() - 50*time.Second}).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("CodeGraph execution stopped: %w", ctx.Err())
		}
		return nil, errors.New("CodeGraph engine connection failed or request timed out")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CodeGraph engine returned %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil || !json.Valid(raw) {
		return nil, errors.New("Invalid graph engine response")
	}
	return raw, nil
}

// Source publication excludes repository metadata, hidden files, credentials,
// symlinks, and dependency directories before either extraction or file reads.
func codeGraphPrivatePath(path string) bool {
	return codegraph.PrivatePath(path, false)
}

func (h *Handler) buildCodeGraph(ctx, terminationCtx context.Context, repo codeGraphRepositoryRow, build pgtype.UUID, pipeline *codegraph.Pipeline) error {
	budget := codegraph.ActiveBudget{Remaining: codegraph.BuildTimeout()}
	runStage := func(phase string, gate chan struct{}, run func(context.Context) error) error {
		if err := h.codeGraphPhase(ctx, repo.ID, build, "waiting_"+phase); err != nil {
			return err
		}
		queued := time.Now()
		return budget.Run(ctx, gate, func(stageCtx context.Context) error {
			waited := time.Since(queued)
			started := time.Now()
			defer func() {
				slog.Info("code graph stage timing", "build_id", uuidToString(build), "phase", phase, "wait_ms", waited.Milliseconds(), "active_ms", time.Since(started).Milliseconds())
			}()
			if err := h.codeGraphPhase(stageCtx, repo.ID, build, phase); err != nil {
				return err
			}
			err := run(stageCtx)
			if err != nil {
				if strings.Contains(err.Error(), "stop process tree:") {
					_, _ = h.DB.Exec(terminationCtx, `UPDATE code_graph_repository SET internal_status='cancelling',updated_at=now() WHERE id=$1`, repo.ID)
					<-terminationCtx.Done()
					return err
				}
				// Retain the resource slot until the failed engine process has exited.
				for terminationCtx.Err() == nil {
					if h.stopCodeGraphEngine(terminationCtx, build) == nil {
						break
					}
					select {
					case <-terminationCtx.Done():
					case <-time.After(time.Second):
					}
				}
			}
			return err
		})
	}
	dir := codeGraphWorkDir(uuidToString(repo.WorkspaceID), uuidToString(repo.ID), uuidToString(build))
	var commit []byte
	err := runStage("cloning", pipeline.Clone, func(ctx context.Context) error {
		if err := os.MkdirAll(filepath.Dir(dir), 0755); err != nil {
			return errors.New("Cannot prepare graph storage")
		}
		authEnv, cleanup, _, err := h.codeGraphGitAuthEnv(ctx, repo.WorkspaceID, repo.RepoURL, repo.Branch)
		if err != nil {
			return errors.New("Cannot prepare Git credentials")
		}
		defer cleanup()
		cmd := exec.Command("git", "-c", "credential.helper=", "-c", "http.extraHeader=", "-c", "http.followRedirects=false", "-c", "http.sslVerify=true", "clone", "--depth=1", "--branch="+repo.Branch, "--", repo.RepoURL, dir)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		cmd.Env = append(cmd.Env, authEnv...)
		if _, err := processtree.CombinedOutput(ctx, cmd, 5*time.Second); err != nil {
			// Remote error text may echo authentication material, even in encoded form.
			return fmt.Errorf("Git clone failed (%w); check Git credentials, repository permissions, branch and network", err)
		}
		commit, err = processtree.Output(ctx, exec.Command("git", "-C", dir, "rev-parse", "HEAD"), 5*time.Second)
		if err != nil {
			return errors.New("Cannot resolve Git commit")
		}
		return nil
	})
	if err != nil {
		return err
	}
	var raw json.RawMessage
	var documents *codegraph.DocumentIndex
	err = runStage("extracting", pipeline.Extract, func(ctx context.Context) error {
		if err := h.codeGraphPhase(ctx, repo.ID, build, "preparing"); err != nil {
			return err
		}
		if _, err := h.codeGraphEngine(ctx, map[string]any{"op": "prepare", "workspace_id": uuidToString(repo.WorkspaceID), "repository_id": uuidToString(repo.ID), "build_id": uuidToString(build)}); err != nil {
			return errors.New("Cannot prepare source snapshot")
		}
		if err := h.codeGraphPhase(ctx, repo.ID, build, "extracting"); err != nil {
			return err
		}
		var err error
		documents, err = codegraph.IndexDocuments(ctx, dir, uuidToString(build), strings.TrimSpace(string(commit)))
		if err != nil {
			return fmt.Errorf("Cannot index documents: %w", err)
		}
		if err = documents.Save(filepath.Join(filepath.Dir(dir), "documents.json")); err != nil {
			return err
		}
		raw, err = h.codeGraphEngine(ctx, map[string]any{"op": "build", "workspace_id": uuidToString(repo.WorkspaceID), "repository_id": uuidToString(repo.ID), "build_id": uuidToString(build)})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	var result struct {
		Data struct {
			Files   int32 `json:"files"`
			Symbols int32 `json:"symbols"`
			Edges   int32 `json:"edges"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || (result.Data.Symbols == 0 && len(documents.Nodes) == 0) {
		return errors.New("No supported code or document structure extracted; previous successful build retained")
	}
	return runStage("publishing", pipeline.Publish, func(ctx context.Context) error {
		if _, err := h.codeGraphEngine(ctx, map[string]any{"op": "publish", "workspace_id": uuidToString(repo.WorkspaceID), "repository_id": uuidToString(repo.ID), "build_id": uuidToString(build)}); err != nil {
			return fmt.Errorf("Cannot publish graph snapshot: %w", err)
		}
		tx, err := h.TxStarter.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SELECT id FROM code_graph_repository WHERE id=$1 FOR UPDATE`, repo.ID); err != nil {
			return err
		}
		var canPublish bool
		if err = tx.QueryRow(ctx, `SELECT status='processing' AND NOT cancel_requested FROM code_graph_build WHERE id=$1 FOR UPDATE`, build).Scan(&canPublish); err != nil {
			return err
		}
		if !canPublish {
			return context.Canceled
		}
		tag, err := tx.Exec(ctx, `UPDATE code_graph_repository SET status='ready',internal_status='indexed',active_build_id=$2,commit_hash=$3,file_count=$4,symbol_count=$5,edge_count=$6,sync_error=NULL,last_indexed_at=now(),updated_at=now() WHERE id=$1`, repo.ID, build, strings.TrimSpace(string(commit)), result.Data.Files, result.Data.Symbols, result.Data.Edges)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errors.New("Repository removed during build")
		}
		_, err = tx.Exec(ctx, `UPDATE code_graph_build SET status='ready',commit_hash=$2,file_count=$3,symbol_count=$4,edge_count=$5,finished_at=now() WHERE id=$1`, build, strings.TrimSpace(string(commit)), result.Data.Files, result.Data.Symbols, result.Data.Edges)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}
