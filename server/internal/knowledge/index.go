package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type IndexBuildResult struct {
	IndexID   string `json:"index_id"`
	Job       *Job   `json:"job,omitempty"`
	Dimension int    `json:"dimension"`
}

// CreateIndex probes the selected embedding binding, records an immutable
// vector-space snapshot, and queues a complete rebuild. The old active index
// remains untouched until the activate stage verifies every current chunk.
func (s *Service) CreateIndex(ctx context.Context, workspaceID, actorID, baseID string) (IndexBuildResult, error) {
	if err := s.checkEnabled(); err != nil {
		return IndexBuildResult{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return IndexBuildResult{}, err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return IndexBuildResult{}, err
	}
	binding, err := s.resolveBinding(ctx, workspaceID, baseID, PurposeEmbedding)
	if err != nil {
		return IndexBuildResult{}, err
	}
	if binding == nil {
		return IndexBuildResult{}, knowledgeError(http.StatusServiceUnavailable, "model_not_configured", "embedding model is not configured", nil)
	}
	base, err := parseID(baseID, "knowledge_base_id")
	if err != nil {
		return IndexBuildResult{}, err
	}
	workspace, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return IndexBuildResult{}, err
	}
	var initialBuilding pgtype.Text
	if err := s.db.QueryRow(ctx, `SELECT building_index_id::text FROM knowledge_base WHERE id=$1`, base).Scan(&initialBuilding); err != nil {
		return IndexBuildResult{}, internal("failed to load index build state", err)
	}
	if initialBuilding.Valid && initialBuilding.String != "" {
		return IndexBuildResult{}, conflict("index_build_in_progress", "an index rebuild is already in progress")
	}
	var sample string
	if err := s.db.QueryRow(ctx, `SELECT c.text FROM knowledge_chunk c JOIN knowledge_document d ON d.id=c.document_id WHERE c.knowledge_base_id=$1 AND d.current_version_id=c.version_id ORDER BY c.id LIMIT 1`, base).Scan(&sample); errors.Is(err, pgx.ErrNoRows) {
		sample = "knowledge embedding probe"
	} else if err != nil {
		return IndexBuildResult{}, internal("failed to load embedding probe text", err)
	}
	vectors, err := s.compatibleClient(binding).Embeddings(ctx, *binding.Binding.Model, []string{sample})
	if err != nil {
		return IndexBuildResult{}, knowledgeError(http.StatusBadGateway, classifyProviderError(err), "embedding provider probe failed", err)
	}
	if len(vectors) != 1 {
		return IndexBuildResult{}, badRequest("model_incompatible", "embedding provider returned no vector")
	}
	literal, err := VectorLiteral(vectors[0], 0)
	if err != nil {
		return IndexBuildResult{}, knowledgeError(http.StatusUnprocessableEntity, "model_incompatible", "embedding provider returned an invalid vector", err)
	}
	dimension := len(vectors[0])
	_ = literal
	fingerprint := EmbeddingFingerprint(binding.Provider.ID, *binding.Binding.Model, dimension, EmbeddingInputVersion, TokenizerVersion)
	snapshot, _ := json.Marshal(map[string]any{"provider_id": binding.Provider.ID, "model": *binding.Binding.Model, "dimension": dimension, "input_template": EmbeddingInputVersion, "normalization": TokenizerVersion, "secret_revision": binding.SecretRevision})
	indexID, indexString := newID()
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return IndexBuildResult{}, internal("failed to start index build", err)
	}
	defer tx.Rollback(ctx)
	if err = lockKnowledgeWorkspace(ctx, tx, workspace); err != nil {
		return IndexBuildResult{}, err
	}
	var corpusRevision int64
	var building pgtype.Text
	if err = tx.QueryRow(ctx, `SELECT corpus_revision,building_index_id::text FROM knowledge_base WHERE id=$1 FOR UPDATE`, base).Scan(&corpusRevision, &building); err != nil {
		return IndexBuildResult{}, internal("failed to lock knowledge base for index build", err)
	}
	if building.Valid && building.String != "" {
		return IndexBuildResult{}, conflict("index_build_in_progress", "an index rebuild is already in progress")
	}
	provider, err := parseID(binding.Provider.ID, "provider_id")
	if err != nil {
		return IndexBuildResult{}, err
	}
	var lockedProvider pgtype.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM knowledge_provider WHERE id=$1 AND workspace_id=$2 AND is_enabled FOR SHARE`, provider, workspace).Scan(&lockedProvider); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IndexBuildResult{}, knowledgeError(http.StatusServiceUnavailable, "provider_disabled", "embedding provider is no longer available", nil)
		}
		return IndexBuildResult{}, internal("failed to lock embedding provider", err)
	}
	var pending int
	if err = tx.QueryRow(ctx, `SELECT count(*)::int FROM knowledge_job WHERE knowledge_base_id=$1 AND stage IN ('fetch','parse','chunk') AND status IN ('queued','running','waiting_config')`, base).Scan(&pending); err != nil {
		return IndexBuildResult{}, internal("failed to check pending document jobs", err)
	}
	if pending > 0 {
		return IndexBuildResult{}, conflict("documents_processing", "wait for document processing to finish before rebuilding the index")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO knowledge_index(id,workspace_id,knowledge_base_id,status,embedding_snapshot,embedding_fingerprint,dimension,corpus_revision) VALUES($1,$2,$3,'building',$4,$5,$6,$7)`, indexID, workspace, base, snapshot, fingerprint, dimension, corpusRevision); err != nil {
		return IndexBuildResult{}, internal("failed to create embedding index", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE knowledge_base SET building_index_id=$2,revision=revision+1,updated_at=now() WHERE id=$1`, base, indexID); err != nil {
		return IndexBuildResult{}, internal("failed to mark embedding index building", err)
	}
	rows, err := tx.Query(ctx, `SELECT current_version_id::text FROM knowledge_document WHERE knowledge_base_id=$1 AND deleted_at IS NULL AND current_version_id IS NOT NULL`, base)
	if err != nil {
		return IndexBuildResult{}, internal("failed to list versions for index build", err)
	}
	versionIDs := []pgtype.UUID{}
	for rows.Next() {
		var value string
		if scanErr := rows.Scan(&value); scanErr == nil {
			if id, parseErr := parseID(value, "version_id"); parseErr == nil {
				versionIDs = append(versionIDs, id)
			}
		}
	}
	rows.Close()
	for _, version := range versionIDs {
		if _, err = s.enqueueJob(ctx, tx, workspace, base, &version, &indexID, "embed", jobKey("embed", uuidString(version), indexString, fingerprint), map[string]any{"version_id": uuidString(version)}); err != nil {
			return IndexBuildResult{}, internal("failed to enqueue index embedding", err)
		}
	}
	job, err := s.enqueueJob(ctx, tx, workspace, base, nil, &indexID, "activate", jobKey("activate", "", indexString, fingerprint), map[string]any{"index_id": indexString})
	if err != nil {
		return IndexBuildResult{}, internal("failed to enqueue index activation", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return IndexBuildResult{}, internal("failed to commit index build", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	return IndexBuildResult{IndexID: indexString, Job: &job, Dimension: dimension}, nil
}
