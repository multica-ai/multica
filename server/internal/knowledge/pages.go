package knowledge

import (
	"context"
	"fmt"
	"strings"
)

func (s *Service) ListBasesPage(ctx context.Context, workspaceID, actorID, cursor string, limit int) (BasePage, error) {
	if err := s.checkEnabled(); err != nil {
		return BasePage{}, err
	}
	ws, err := parseID(workspaceID, "workspace_id")
	if err != nil {
		return BasePage{}, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return BasePage{}, err
	}
	limit = knowledgePageLimit(limit)
	query := `
		SELECT id::text, workspace_id::text, creator_id::text, name, description,
		       visibility, revision, acl_revision, corpus_revision, active_index_id::text,
		       created_at, updated_at
		FROM knowledge_base
		WHERE workspace_id=$1 AND deleted_at IS NULL
		  AND EXISTS (SELECT 1 FROM member m WHERE m.workspace_id=knowledge_base.workspace_id AND m.user_id=$2)
		  AND (visibility='workspace' OR (creator_id=$2 AND $3::boolean))`
	args := []any{ws, actor, privateAccessAllowed(ctx)}
	if stringsTrimmed := cursor; stringsTrimmed != "" {
		createdAt, id, cursorErr := decodeKnowledgeCreatedCursor(stringsTrimmed)
		if cursorErr != nil {
			return BasePage{}, cursorErr
		}
		query += ` AND (created_at < $4 OR (created_at=$4 AND id < $5))`
		args = append(args, createdAt, id)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return BasePage{}, internal("failed to list knowledge bases", err)
	}
	defer rows.Close()
	items := make([]Base, 0, limit)
	for rows.Next() {
		item, scanErr := scanBase(rows)
		if scanErr != nil {
			return BasePage{}, internal("failed to read knowledge base", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return BasePage{}, internal("failed to list knowledge bases", err)
	}
	page := BasePage{Bases: items}
	if len(items) > limit {
		page.Bases = items[:limit]
		last := page.Bases[len(page.Bases)-1]
		page.NextCursor = encodeKnowledgeCreatedCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

func (s *Service) ListDocumentsPage(ctx context.Context, workspaceID, actorID, baseID, cursor string, limit int) (DocumentPage, error) {
	if err := s.checkEnabled(); err != nil {
		return DocumentPage{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return DocumentPage{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return DocumentPage{}, err
	}
	limit = knowledgePageLimit(limit)
	query := `
		SELECT d.id::text, d.workspace_id::text, d.knowledge_base_id::text, d.title,
		       d.source_kind, d.source_url, d.tags, d.current_version_id::text,
		       d.revision, COALESCE(v.status,'processing'), d.deleted_at,
		       d.created_at, d.updated_at
		FROM knowledge_document d
		LEFT JOIN knowledge_document_version v ON v.id=d.current_version_id
		WHERE d.knowledge_base_id=$1 AND d.deleted_at IS NULL`
	args := []any{params.base}
	if cursor != "" {
		createdAt, id, cursorErr := decodeKnowledgeCreatedCursor(cursor)
		if cursorErr != nil {
			return DocumentPage{}, cursorErr
		}
		query += ` AND (d.created_at < $2 OR (d.created_at=$2 AND d.id < $3))`
		args = append(args, createdAt, id)
	}
	query += fmt.Sprintf(" ORDER BY d.created_at DESC, d.id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return DocumentPage{}, internal("failed to list knowledge documents", err)
	}
	defer rows.Close()
	items := make([]Document, 0, limit)
	for rows.Next() {
		item, scanErr := scanDocument(rows)
		if scanErr != nil {
			return DocumentPage{}, internal("failed to read knowledge document", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return DocumentPage{}, internal("failed to list knowledge documents", err)
	}
	page := DocumentPage{Documents: items}
	if len(items) > limit {
		page.Documents = items[:limit]
		last := page.Documents[len(page.Documents)-1]
		page.NextCursor = encodeKnowledgeCreatedCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

func (s *Service) ListVersionsPage(ctx context.Context, workspaceID, actorID, baseID, documentID, cursor string, limit int) (VersionPage, error) {
	if err := s.checkEnabled(); err != nil {
		return VersionPage{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return VersionPage{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return VersionPage{}, err
	}
	doc, err := parseID(documentID, "document_id")
	if err != nil {
		return VersionPage{}, err
	}
	limit = knowledgePageLimit(limit)
	query := `
		SELECT v.id::text, v.document_id::text, v.version_number, v.source_object_key,
		       v.source_hash, v.byte_size, v.mime_type, v.source_metadata, v.parsed_object_key,
		       v.parser_version, v.chunker_version, v.config_snapshot, v.status, v.error_code,
		       v.created_at
		FROM knowledge_document_version v JOIN knowledge_document d ON d.id=v.document_id
		WHERE v.document_id=$1 AND v.knowledge_base_id=$2 AND d.deleted_at IS NULL`
	args := []any{doc, params.base}
	if strings.TrimSpace(cursor) != "" {
		createdAt, id, cursorErr := decodeKnowledgeCreatedCursor(cursor)
		if cursorErr != nil {
			return VersionPage{}, cursorErr
		}
		query += ` AND (v.created_at<$3 OR (v.created_at=$3 AND v.id<$4))`
		args = append(args, createdAt, id)
	}
	query += fmt.Sprintf(" ORDER BY v.created_at DESC,v.id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return VersionPage{}, internal("failed to list document versions", err)
	}
	defer rows.Close()
	versions := make([]DocumentVersion, 0, limit)
	for rows.Next() {
		version, scanErr := scanVersion(rows)
		if scanErr != nil {
			return VersionPage{}, internal("failed to read document version", scanErr)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return VersionPage{}, internal("failed to list document versions", err)
	}
	page := VersionPage{Versions: versions}
	if len(versions) > limit {
		page.Versions = versions[:limit]
		last := page.Versions[len(page.Versions)-1]
		page.NextCursor = encodeKnowledgeCreatedCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

func (s *Service) ListJobsPage(ctx context.Context, workspaceID, actorID, baseID, cursor string, limit int) (JobPage, error) {
	if err := s.checkEnabled(); err != nil {
		return JobPage{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return JobPage{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return JobPage{}, err
	}
	limit = knowledgePageLimit(limit)
	query := `
		SELECT id::text, knowledge_base_id::text, document_version_id::text, index_id::text,
		       stage, status, attempt, available_at, progress, error_code, created_at
		FROM knowledge_job WHERE knowledge_base_id=$1`
	args := []any{params.base}
	if cursor != "" {
		createdAt, id, cursorErr := decodeKnowledgeCreatedCursor(cursor)
		if cursorErr != nil {
			return JobPage{}, cursorErr
		}
		query += ` AND (created_at < $2 OR (created_at=$2 AND id < $3))`
		args = append(args, createdAt, id)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return JobPage{}, internal("failed to list knowledge jobs", err)
	}
	defer rows.Close()
	items := make([]Job, 0, limit)
	for rows.Next() {
		item, scanErr := scanJob(rows)
		if scanErr != nil {
			return JobPage{}, internal("failed to read knowledge job", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return JobPage{}, internal("failed to list knowledge jobs", err)
	}
	page := JobPage{Jobs: items}
	if len(items) > limit {
		page.Jobs = items[:limit]
		last := page.Jobs[len(page.Jobs)-1]
		page.NextCursor = encodeKnowledgeCreatedCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

func (s *Service) ListGraphEditsPage(ctx context.Context, workspaceID, actorID, baseID, cursor string, limit int) (GraphEditPage, error) {
	if err := s.checkEnabled(); err != nil {
		return GraphEditPage{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return GraphEditPage{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return GraphEditPage{}, err
	}
	limit = knowledgePageLimit(limit)
	query := `SELECT id::text,operation,target_id::text,payload,previous_state,actor_id::text,expected_revision,created_at,reverted_at FROM knowledge_graph_edit WHERE knowledge_base_id=$1`
	args := []any{params.base}
	if strings.TrimSpace(cursor) != "" {
		createdAt, id, cursorErr := decodeKnowledgeCreatedCursor(cursor)
		if cursorErr != nil {
			return GraphEditPage{}, cursorErr
		}
		query += ` AND (created_at<$2 OR (created_at=$2 AND id<$3))`
		args = append(args, createdAt, id)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return GraphEditPage{}, internal("failed to list graph edits", err)
	}
	defer rows.Close()
	edits := make([]GraphEdit, 0, limit)
	for rows.Next() {
		item, scanErr := scanGraphEdit(rows)
		if scanErr != nil {
			return GraphEditPage{}, internal("failed to read graph edit", scanErr)
		}
		edits = append(edits, item)
	}
	if err := rows.Err(); err != nil {
		return GraphEditPage{}, internal("failed to list graph edits", err)
	}
	page := GraphEditPage{Edits: edits}
	if len(edits) > limit {
		page.Edits = edits[:limit]
		last := page.Edits[len(page.Edits)-1]
		page.NextCursor = encodeKnowledgeCreatedCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

func (s *Service) ListEntitiesPage(ctx context.Context, workspaceID, actorID, baseID, queryText, entityType, cursor string, limit int) (EntityPage, error) {
	if err := s.checkEnabled(); err != nil {
		return EntityPage{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return EntityPage{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return EntityPage{}, err
	}
	limit = knowledgePageLimit(limit)
	queryText = strings.TrimSpace(queryText)
	entityType = strings.TrimSpace(entityType)
	if entityType != "" && !contains(SupportedEntityTypes, entityType) {
		return EntityPage{}, badRequest("invalid_entity_type", "unsupported entity type")
	}
	query := `
		SELECT e.id::text,e.knowledge_base_id::text,e.type,e.canonical_name,e.normalized_name,e.review_status,e.revision,
		       (SELECT count(*) FROM knowledge_evidence ev JOIN knowledge_entity mention ON mention.id=ev.subject_id JOIN knowledge_document_version vv ON vv.id=ev.version_id JOIN knowledge_document dd ON dd.id=vv.document_id WHERE ev.subject_type='entity' AND COALESCE(mention.merged_into_id,mention.id)=e.id AND dd.current_version_id=vv.id AND dd.deleted_at IS NULL AND (ev.extraction_run_id IS NULL OR EXISTS (SELECT 1 FROM knowledge_extraction_run er WHERE er.id=ev.extraction_run_id AND er.version_id=vv.id AND er.is_active)))::int
		FROM knowledge_entity e
		WHERE e.knowledge_base_id=$1 AND e.merged_into_id IS NULL AND ` + s.currentEvidencePredicate("e") + `
		  AND ($2='' OR e.canonical_name ILIKE '%'||$2||'%' OR e.normalized_name ILIKE '%'||$2||'%')
		  AND ($3='' OR e.type=$3)`
	args := []any{params.base, queryText, entityType}
	if cursor != "" {
		name, id, cursorErr := decodeKnowledgeEntityCursor(cursor)
		if cursorErr != nil {
			return EntityPage{}, cursorErr
		}
		query += ` AND (e.canonical_name > $4 OR (e.canonical_name=$4 AND e.id > $5))`
		args = append(args, name, id)
	}
	query += fmt.Sprintf(" ORDER BY e.canonical_name,e.id LIMIT $%d", len(args)+1)
	args = append(args, limit+1)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return EntityPage{}, internal("failed to list knowledge entities", err)
	}
	defer rows.Close()
	items := make([]Entity, 0, limit)
	for rows.Next() {
		item, scanErr := scanEntity(rows)
		if scanErr != nil {
			return EntityPage{}, internal("failed to read knowledge entity", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return EntityPage{}, internal("failed to list knowledge entities", err)
	}
	page := EntityPage{Entities: items}
	if len(items) > limit {
		page.Entities = items[:limit]
		last := page.Entities[len(page.Entities)-1]
		page.NextCursor = encodeKnowledgeEntityCursor(last.CanonicalName, last.ID)
	}
	return page, nil
}
