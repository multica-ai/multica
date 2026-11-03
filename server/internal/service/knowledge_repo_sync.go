package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/knowledge"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The repo-docs sync (plan 10.12). A repository's docs directory is kept in one
// knowledge base as documents of kind reference, one document per Markdown file.
// A new file is created. A file whose content changed is replaced: the old
// document leaves WeKnora and the new one is written. A removed file is
// archived: its document leaves WeKnora and its sync record keeps the path with
// archived_at set. The sync is off until a workspace enables it.

const (
	repoSyncKind         = "reference"
	repoSyncDefaultBase  = "engineering"
	repoSyncDefaultPath  = "docs"
	repoSyncDefaultRef   = "main"
	repoSyncMaxFiles     = 500
	repoSyncMaxFileBytes = 1 << 20
	repoSyncTimeout      = 3 * time.Minute
	// repoSyncErrorLimit bounds the failure text kept on the setting.
	repoSyncErrorLimit = 4000
)

var (
	repoBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
	repoKBKeyPattern  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

// RepoSyncSettings is a workspace's repo-docs sync setting.
type RepoSyncSettings struct {
	Enabled  bool   `json:"enabled"`
	RepoURL  string `json:"repo_url"`
	Branch   string `json:"branch"`
	DocsPath string `json:"docs_path"`
	KBKey    string `json:"knowledge_base_key"`
}

// RepoSyncStatus is the setting with the outcome of its last run.
type RepoSyncStatus struct {
	RepoSyncSettings
	LastSyncedAt *time.Time `json:"last_synced_at"`
	LastError    string     `json:"last_error"`
}

// RepoDoc is one file of a docs directory: its path from the repository root
// and its text.
type RepoDoc struct {
	Path    string
	Content string
}

// RepoDocsSource reads the Markdown files under the docs directory of a
// repository branch. Tests inject a fake; production clones with git.
type RepoDocsSource interface {
	Snapshot(ctx context.Context, cfg RepoSyncSettings) ([]RepoDoc, error)
}

// validate checks a setting the way the sync will use it. The docs path is
// relative and stays inside the repository.
func (c RepoSyncSettings) validate() error {
	u, err := url.Parse(c.RepoURL)
	switch {
	case err != nil || u.Scheme != "https" || u.Host == "" || len(c.RepoURL) > 2048 || strings.ContainsAny(c.RepoURL, " \t\r\n"):
		return &KnowledgeInvalidError{"repo_url must be an https address"}
	case !repoBranchPattern.MatchString(c.Branch) || strings.Contains(c.Branch, ".."):
		return &KnowledgeInvalidError{"branch is not a valid branch name"}
	case c.DocsPath == "" || path.IsAbs(c.DocsPath) || strings.Contains(c.DocsPath, `\`) ||
		path.Clean(c.DocsPath) == "." || path.Clean(c.DocsPath) == ".." || strings.HasPrefix(path.Clean(c.DocsPath), "../"):
		return &KnowledgeInvalidError{"docs_path must be a relative path inside the repository"}
	case !repoKBKeyPattern.MatchString(c.KBKey):
		return &KnowledgeInvalidError{"knowledge_base_key must be a knowledge base key such as engineering"}
	}
	return nil
}

// GetRepoSync returns the workspace's setting. A workspace that never set one
// has the sync off, with the default base and paths.
func (s *KnowledgeService) GetRepoSync(ctx context.Context, workspaceID pgtype.UUID) (RepoSyncStatus, error) {
	row, err := s.Queries.GetKnowledgeRepoSync(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RepoSyncStatus{RepoSyncSettings: RepoSyncSettings{
			Branch: repoSyncDefaultRef, DocsPath: repoSyncDefaultPath, KBKey: repoSyncDefaultBase,
		}}, nil
	}
	if err != nil {
		return RepoSyncStatus{}, fmt.Errorf("load repo sync: %w", err)
	}
	return repoSyncStatusOf(row), nil
}

// SetRepoSync writes the workspace's setting. The knowledge base must exist in
// the workspace; empty branch, docs path and base take their defaults.
func (s *KnowledgeService) SetRepoSync(ctx context.Context, workspaceID pgtype.UUID, in RepoSyncSettings) (RepoSyncStatus, error) {
	in.RepoURL = strings.TrimSpace(in.RepoURL)
	in.Branch = strings.TrimSpace(in.Branch)
	in.DocsPath = strings.TrimSpace(in.DocsPath)
	in.KBKey = strings.TrimSpace(in.KBKey)
	if in.Branch == "" {
		in.Branch = repoSyncDefaultRef
	}
	if in.DocsPath == "" {
		in.DocsPath = repoSyncDefaultPath
	}
	if in.KBKey == "" {
		in.KBKey = repoSyncDefaultBase
	}
	in.DocsPath = path.Clean(in.DocsPath)
	if err := in.validate(); err != nil {
		return RepoSyncStatus{}, err
	}
	if _, err := s.Queries.GetKnowledgeBaseByKey(ctx, db.GetKnowledgeBaseByKeyParams{WorkspaceID: workspaceID, Key: in.KBKey}); errors.Is(err, pgx.ErrNoRows) {
		return RepoSyncStatus{}, &KnowledgeInvalidError{"knowledge base " + in.KBKey + " is not in this workspace"}
	} else if err != nil {
		return RepoSyncStatus{}, fmt.Errorf("load knowledge base: %w", err)
	}
	row, err := s.Queries.UpsertKnowledgeRepoSync(ctx, db.UpsertKnowledgeRepoSyncParams{
		WorkspaceID: workspaceID, Enabled: in.Enabled, RepoUrl: in.RepoURL,
		Branch: in.Branch, DocsPath: in.DocsPath, KbKey: in.KBKey,
	})
	if err != nil {
		return RepoSyncStatus{}, fmt.Errorf("save repo sync: %w", err)
	}
	return repoSyncStatusOf(row), nil
}

func repoSyncStatusOf(row db.KnowledgeRepoSync) RepoSyncStatus {
	out := RepoSyncStatus{
		RepoSyncSettings: RepoSyncSettings{
			Enabled: row.Enabled, RepoURL: row.RepoUrl, Branch: row.Branch, DocsPath: row.DocsPath, KBKey: row.KbKey,
		},
		LastError: row.LastError,
	}
	if row.LastSyncedAt.Valid {
		t := row.LastSyncedAt.Time
		out.LastSyncedAt = &t
	}
	return out
}

// SyncRepoDocs brings one workspace's docs directory into its knowledge base.
// It does nothing unless the sync is enabled. A failure of one file is recorded
// on the setting and retried by the next run; only a failure to read the
// repository or the knowledge service is returned.
func (s *KnowledgeService) SyncRepoDocs(ctx context.Context, workspaceID pgtype.UUID) error {
	row, err := s.Queries.GetKnowledgeRepoSync(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load repo sync: %w", err)
	}
	if !row.Enabled {
		return nil
	}
	cfg := repoSyncStatusOf(row).RepoSyncSettings
	if _, err := s.workspaceClient(ctx, workspaceID); err != nil {
		return err
	}
	docs, err := s.repoDocsSource().Snapshot(ctx, cfg)
	if err != nil {
		s.recordRepoSyncRun(ctx, workspaceID, err.Error())
		return err
	}
	recorded, err := s.Queries.ListKnowledgeRepoFiles(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list synced files: %w", err)
	}
	byPath := make(map[string]db.KnowledgeRepoFile, len(recorded))
	for _, f := range recorded {
		byPath[f.Path] = f
	}

	var failures []string
	seen := map[string]bool{}
	for _, d := range docs {
		seen[d.Path] = true
		sum := sha256.Sum256([]byte(d.Content))
		hash := hex.EncodeToString(sum[:])
		prev, have := byPath[d.Path]
		if have && !prev.ArchivedAt.Valid && prev.ContentHash == hash {
			continue
		}
		if have && !prev.ArchivedAt.Valid && prev.KnowledgeLinkID.Valid {
			// The old version leaves WeKnora before the new one is written.
			if err := s.removeSyncedDocument(ctx, workspaceID, prev.KnowledgeLinkID); err != nil {
				failures = append(failures, d.Path+": "+err.Error())
				continue
			}
		}
		view, _, err := s.CreateDocument(ctx, workspaceID, KnowledgeActor{Type: KnowledgeActorSystem}, KnowledgeDocumentInput{
			KB: cfg.KBKey, Kind: repoSyncKind, Title: truncateRunes(d.Path, knowledgeMaxTitle), Content: d.Content,
			SourceType: "sync", IdempotencyKey: "repo-sync:" + d.Path + ":" + hash, Publish: true,
		})
		if err != nil {
			failures = append(failures, d.Path+": "+err.Error())
			continue
		}
		linkID, err := util.ParseUUID(view.ID)
		if err != nil {
			return fmt.Errorf("repo sync: document id: %w", err)
		}
		if err := s.Queries.UpsertKnowledgeRepoFile(ctx, db.UpsertKnowledgeRepoFileParams{
			WorkspaceID: workspaceID, Path: d.Path, KnowledgeLinkID: linkID, ContentHash: hash,
		}); err != nil {
			return fmt.Errorf("record synced file: %w", err)
		}
	}

	// A file the repository no longer has is archived.
	for p, prev := range byPath {
		if seen[p] || prev.ArchivedAt.Valid {
			continue
		}
		if prev.KnowledgeLinkID.Valid {
			if err := s.removeSyncedDocument(ctx, workspaceID, prev.KnowledgeLinkID); err != nil {
				failures = append(failures, p+": "+err.Error())
				continue
			}
		}
		if err := s.Queries.ArchiveKnowledgeRepoFile(ctx, db.ArchiveKnowledgeRepoFileParams{WorkspaceID: workspaceID, Path: p}); err != nil {
			return fmt.Errorf("archive synced file: %w", err)
		}
	}

	s.recordRepoSyncRun(ctx, workspaceID, strings.Join(failures, "; "))
	return nil
}

// removeSyncedDocument deletes a synced document. One already gone is not an
// error: the record it left is what the sync is catching up on.
func (s *KnowledgeService) removeSyncedDocument(ctx context.Context, workspaceID, linkID pgtype.UUID) error {
	err := s.DeleteDocument(ctx, workspaceID, linkID)
	if errors.Is(err, ErrKnowledgeDocumentNotFound) {
		return nil
	}
	return err
}

// recordRepoSyncRun stores the outcome of a run on the setting. The text is
// bounded; an empty text means the run had no failures.
func (s *KnowledgeService) recordRepoSyncRun(ctx context.Context, workspaceID pgtype.UUID, text string) {
	text = truncateRunes(text, repoSyncErrorLimit)
	if err := s.Queries.SetKnowledgeRepoSyncRun(ctx, db.SetKnowledgeRepoSyncRunParams{WorkspaceID: workspaceID, LastError: text}); err != nil {
		slog.Warn("knowledge: could not record the repo docs sync run", "workspace_id", util.UUIDToString(workspaceID), "error", err)
	}
}

func (s *KnowledgeService) repoDocsSource() RepoDocsSource {
	if s.RepoDocs != nil {
		return s.RepoDocs
	}
	return gitRepoDocs{}
}

// RunRepoDocsSync runs the sync for every workspace with ready knowledge. It
// stops on a WeKnora outage so the scheduler retries the whole pass; any other
// failure is logged and the pass goes on.
func (s *KnowledgeService) RunRepoDocsSync(ctx context.Context) (int, error) {
	if s == nil {
		return 0, nil
	}
	ids, err := s.Queries.ListReadyKnowledgeWorkspaceIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list knowledge workspaces: %w", err)
	}
	var failures []error
	done := 0
	for _, id := range ids {
		err := s.SyncRepoDocs(ctx, id)
		if err == nil {
			done++
			continue
		}
		if errors.Is(err, knowledge.ErrUnavailable) {
			return done, err
		}
		slog.Warn("knowledge: repo docs sync failed for a workspace", "workspace_id", util.UUIDToString(id), "error", err)
		failures = append(failures, fmt.Errorf("workspace %s: %w", util.UUIDToString(id), err))
	}
	return done, errors.Join(failures...)
}

// gitRepoDocs clones the branch shallowly into a temporary directory and reads
// the docs directory from it. The git binary runs with a fixed argument list, a
// timeout, and no prompts. Only https addresses are accepted.
type gitRepoDocs struct{}

func (gitRepoDocs) Snapshot(ctx context.Context, cfg RepoSyncSettings) ([]RepoDoc, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "multica-repo-docs-")
	if err != nil {
		return nil, fmt.Errorf("repo sync: temporary directory: %w", err)
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, repoSyncTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--single-branch", "--branch", cfg.Branch, "--", cfg.RepoURL, dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("repo sync: git clone failed: %w: %s", err, truncateRunes(strings.TrimSpace(string(out)), 500))
	}
	return readRepoDocs(dir, cfg.DocsPath)
}

// readRepoDocs reads the Markdown files under docsPath of a checkout. Symlinks
// are skipped, so a link cannot reach a file outside the repository. Blank files
// and files over the size limit are skipped; too many files is an error.
func readRepoDocs(root, docsPath string) ([]RepoDoc, error) {
	base := filepath.Join(root, filepath.FromSlash(docsPath))
	var docs []RepoDoc
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() || !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > repoSyncMaxFileBytes {
			slog.Warn("knowledge: repo docs sync skipped a file over the size limit", "path", p, "bytes", info.Size())
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(body)) == "" {
			return nil
		}
		if len(docs) >= repoSyncMaxFiles {
			return fmt.Errorf("repo sync: more than %d Markdown files under %s", repoSyncMaxFiles, docsPath)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		docs = append(docs, RepoDoc{Path: filepath.ToSlash(rel), Content: string(body)})
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &KnowledgeInvalidError{"docs_path " + docsPath + " is not a directory in the repository"}
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	return docs, nil
}
