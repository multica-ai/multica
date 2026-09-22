package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func scanEntity(row pgx.Row) (Entity, error) {
	var entity Entity
	return entity, row.Scan(&entity.ID, &entity.KnowledgeBaseID, &entity.Type, &entity.CanonicalName, &entity.NormalizedName, &entity.ReviewStatus, &entity.Revision, &entity.EvidenceCount)
}

func scanRelation(row pgx.Row) (Relation, error) {
	var relation Relation
	var qualifier []byte
	err := row.Scan(&relation.ID, &relation.KnowledgeBaseID, &relation.SourceEntityID, &relation.TargetEntityID, &relation.Predicate, &qualifier, &relation.ReviewStatus, &relation.Revision, &relation.EvidenceCount)
	if err != nil {
		return Relation{}, err
	}
	relation.Qualifier = mapJSON(qualifier)
	return relation, nil
}

func scanEvidence(row pgx.Row) (Evidence, error) {
	var evidence Evidence
	var locator []byte
	var start, end pgtype.Int4
	var runID pgtype.Text
	err := row.Scan(&evidence.ID, &evidence.SubjectType, &evidence.SubjectID, &evidence.VersionID, &evidence.ChunkID, &locator, &evidence.Quote, &start, &end, &runID)
	if err != nil {
		return Evidence{}, err
	}
	evidence.SourceLocator = mapJSON(locator)
	if start.Valid {
		value := int(start.Int32)
		evidence.StartOffset = &value
	}
	if end.Valid {
		value := int(end.Int32)
		evidence.EndOffset = &value
	}
	evidence.ExtractionRunID = nullableText(runID)
	return evidence, nil
}

func (s *Service) currentEvidencePredicate(alias string) string {
	return `EXISTS (SELECT 1 FROM knowledge_evidence ev JOIN knowledge_entity mention ON mention.id=ev.subject_id JOIN knowledge_document_version vv ON vv.id=ev.version_id JOIN knowledge_document dd ON dd.id=vv.document_id WHERE ev.subject_type='entity' AND COALESCE(mention.merged_into_id,mention.id)=` + alias + `.id AND dd.current_version_id=vv.id AND dd.deleted_at IS NULL AND (ev.extraction_run_id IS NULL OR EXISTS (SELECT 1 FROM knowledge_extraction_run er WHERE er.id=ev.extraction_run_id AND er.version_id=vv.id AND er.is_active)))`
}

func (s *Service) ListEntities(ctx context.Context, workspaceID, actorID, baseID, query, entityType string, limit int) ([]Entity, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query = strings.TrimSpace(query)
	entityType = strings.TrimSpace(entityType)
	if entityType != "" && !contains(SupportedEntityTypes, entityType) {
		return nil, badRequest("invalid_entity_type", "unsupported entity type")
	}
	rows, err := s.db.Query(ctx, `
		SELECT e.id::text,e.knowledge_base_id::text,e.type,e.canonical_name,e.normalized_name,e.review_status,e.revision,
		       (SELECT count(*) FROM knowledge_evidence ev JOIN knowledge_entity mention ON mention.id=ev.subject_id JOIN knowledge_document_version vv ON vv.id=ev.version_id JOIN knowledge_document dd ON dd.id=vv.document_id WHERE ev.subject_type='entity' AND COALESCE(mention.merged_into_id,mention.id)=e.id AND dd.current_version_id=vv.id AND dd.deleted_at IS NULL AND (ev.extraction_run_id IS NULL OR EXISTS (SELECT 1 FROM knowledge_extraction_run er WHERE er.id=ev.extraction_run_id AND er.version_id=vv.id AND er.is_active)))::int AS evidence_count
		FROM knowledge_entity e
		WHERE e.knowledge_base_id=$1 AND e.merged_into_id IS NULL AND `+s.currentEvidencePredicate("e")+`
		  AND ($2='' OR e.canonical_name ILIKE '%'||$2||'%' OR e.normalized_name ILIKE '%'||$2||'%')
		  AND ($3='' OR e.type=$3)
		ORDER BY evidence_count DESC,e.canonical_name,e.id LIMIT $4`, params.base, query, entityType, limit)
	if err != nil {
		return nil, internal("failed to list knowledge entities", err)
	}
	defer rows.Close()
	entities := []Entity{}
	for rows.Next() {
		entity, scanErr := scanEntity(rows)
		if scanErr != nil {
			return nil, internal("failed to read knowledge entity", scanErr)
		}
		entities = append(entities, entity)
	}
	if err := rows.Err(); err != nil {
		return nil, internal("failed to list knowledge entities", err)
	}
	return entities, nil
}

func (s *Service) GetEntity(ctx context.Context, workspaceID, actorID, baseID, entityID string) (EntityDetail, error) {
	if err := s.checkEnabled(); err != nil {
		return EntityDetail{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return EntityDetail{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return EntityDetail{}, err
	}
	id, err := parseID(entityID, "entity_id")
	if err != nil {
		return EntityDetail{}, err
	}
	if err := s.db.QueryRow(ctx, `SELECT COALESCE(merged_into_id,id) FROM knowledge_entity WHERE id=$1 AND knowledge_base_id=$2`, id, params.base).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EntityDetail{}, notFound()
		}
		return EntityDetail{}, internal("failed to resolve knowledge entity", err)
	}
	entity, err := scanEntity(s.db.QueryRow(ctx, `
		SELECT e.id::text,e.knowledge_base_id::text,e.type,e.canonical_name,e.normalized_name,e.review_status,e.revision,
		       (SELECT count(*) FROM knowledge_evidence ev JOIN knowledge_entity mention ON mention.id=ev.subject_id JOIN knowledge_document_version vv ON vv.id=ev.version_id JOIN knowledge_document dd ON dd.id=vv.document_id WHERE ev.subject_type='entity' AND COALESCE(mention.merged_into_id,mention.id)=e.id AND dd.current_version_id=vv.id AND dd.deleted_at IS NULL AND (ev.extraction_run_id IS NULL OR EXISTS (SELECT 1 FROM knowledge_extraction_run er WHERE er.id=ev.extraction_run_id AND er.version_id=vv.id AND er.is_active)))::int AS evidence_count
		FROM knowledge_entity e WHERE e.id=$1 AND e.knowledge_base_id=$2 AND `+s.currentEvidencePredicate("e"), id, params.base))
	if errors.Is(err, pgx.ErrNoRows) {
		return EntityDetail{}, notFound()
	}
	if err != nil {
		return EntityDetail{}, internal("failed to load knowledge entity", err)
	}
	evidence, err := s.listEvidenceForSubject(ctx, params.base, "entity", id)
	if err != nil {
		return EntityDetail{}, err
	}
	return EntityDetail{Entity: entity, Evidence: evidence}, nil
}

func (s *Service) listEvidenceForSubject(ctx context.Context, base pgtype.UUID, subjectType string, subjectID pgtype.UUID) ([]Evidence, error) {
	joinEntity := ""
	subjectPredicate := "ev.subject_id=$3"
	if subjectType == "entity" {
		joinEntity = " JOIN knowledge_entity mention ON mention.id=ev.subject_id"
		subjectPredicate = "COALESCE(mention.merged_into_id,mention.id)=$3"
	}
	rows, err := s.db.Query(ctx, `
		SELECT ev.id::text,ev.subject_type,ev.subject_id::text,ev.version_id::text,ev.chunk_id::text,ev.source_locator,ev.quote,ev.start_offset,ev.end_offset,ev.extraction_run_id::text
		FROM knowledge_evidence ev`+joinEntity+` JOIN knowledge_document_version vv ON vv.id=ev.version_id JOIN knowledge_document dd ON dd.id=vv.document_id
		WHERE ev.knowledge_base_id=$1 AND ev.subject_type=$2 AND `+subjectPredicate+` AND dd.current_version_id=vv.id AND dd.deleted_at IS NULL
		  AND (ev.extraction_run_id IS NULL OR EXISTS (SELECT 1 FROM knowledge_extraction_run er WHERE er.id=ev.extraction_run_id AND er.version_id=vv.id AND er.is_active))
		ORDER BY ev.created_at DESC`, base, subjectType, subjectID)
	if err != nil {
		return nil, internal("failed to load knowledge evidence", err)
	}
	defer rows.Close()
	items := []Evidence{}
	for rows.Next() {
		item, scanErr := scanEvidence(rows)
		if scanErr != nil {
			return nil, internal("failed to read knowledge evidence", scanErr)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type GraphQuery struct {
	EntityID   string
	Depth      int
	Predicate  string
	DocumentID string
	NodeLimit  int
}

func (s *Service) Graph(ctx context.Context, workspaceID, actorID, baseID string, input GraphQuery) (GraphResponse, error) {
	if err := s.checkEnabled(); err != nil {
		return GraphResponse{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return GraphResponse{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return GraphResponse{}, err
	}
	nodeLimit := input.NodeLimit
	if nodeLimit <= 0 || nodeLimit > 200 {
		nodeLimit = 200
	}
	depth := input.Depth
	if depth <= 0 {
		depth = 1
	}
	if depth > 2 {
		depth = 2
	}
	entityID := pgtype.UUID{}
	entityID = pgtype.UUID{Bytes: [16]byte{}, Valid: true}
	if strings.TrimSpace(input.EntityID) != "" {
		entityID, err = parseID(input.EntityID, "entity_id")
		if err != nil {
			return GraphResponse{}, err
		}
	}
	predicate := strings.TrimSpace(input.Predicate)
	if predicate != "" && !contains(SupportedPredicates, predicate) {
		return GraphResponse{}, badRequest("invalid_predicate", "unsupported graph predicate")
	}
	docID := pgtype.UUID{Bytes: [16]byte{}, Valid: true}
	if strings.TrimSpace(input.DocumentID) != "" {
		docID, err = parseID(input.DocumentID, "document_id")
		if err != nil {
			return GraphResponse{}, err
		}
	}
	hasEntity := strings.TrimSpace(input.EntityID) != ""
	if hasEntity {
		if err := s.db.QueryRow(ctx, `SELECT COALESCE(merged_into_id,id) FROM knowledge_entity WHERE id=$1 AND knowledge_base_id=$2`, entityID, params.base).Scan(&entityID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return GraphResponse{}, notFound()
			}
			return GraphResponse{}, internal("failed to resolve graph entity", err)
		}
	}
	var nodeRows pgx.Rows
	if hasEntity {
		nodeRows, err = s.db.Query(ctx, `
			WITH RECURSIVE relation_view AS (
				SELECT r.id,r.predicate,
				       COALESCE(source.merged_into_id,source.id) AS source_entity_id,
				       COALESCE(target.merged_into_id,target.id) AS target_entity_id
				FROM knowledge_relation r
				JOIN knowledge_entity source ON source.id=r.source_entity_id
				JOIN knowledge_entity target ON target.id=r.target_entity_id
				WHERE r.knowledge_base_id=$1 AND r.review_status <> 'rejected'
			), walk(id,distance) AS (
				SELECT $2::uuid,0
				UNION
				SELECT CASE WHEN r.source_entity_id=w.id THEN r.target_entity_id ELSE r.source_entity_id END,w.distance+1
				FROM walk w
				JOIN relation_view r ON (r.source_entity_id=w.id OR r.target_entity_id=w.id)
				WHERE w.distance<$4 AND ($3='' OR r.predicate=$3)
				  AND EXISTS (SELECT 1 FROM knowledge_evidence ev JOIN knowledge_document_version vv ON vv.id=ev.version_id JOIN knowledge_document dd ON dd.id=vv.document_id WHERE ev.subject_type='relation' AND ev.subject_id=r.id AND dd.current_version_id=vv.id AND dd.deleted_at IS NULL AND ($5::uuid='00000000-0000-0000-0000-000000000000' OR dd.id=$5))
			)
			SELECT e.id::text,e.knowledge_base_id::text,e.type,e.canonical_name,e.normalized_name,e.review_status,e.revision,
			       (SELECT count(*) FROM knowledge_evidence ev JOIN knowledge_entity mention ON mention.id=ev.subject_id JOIN knowledge_document_version vv ON vv.id=ev.version_id JOIN knowledge_document dd ON dd.id=vv.document_id WHERE ev.subject_type='entity' AND COALESCE(mention.merged_into_id,mention.id)=e.id AND dd.current_version_id=vv.id AND dd.deleted_at IS NULL AND (ev.extraction_run_id IS NULL OR EXISTS (SELECT 1 FROM knowledge_extraction_run er WHERE er.id=ev.extraction_run_id AND er.version_id=vv.id AND er.is_active)))::int AS evidence_count
			FROM knowledge_entity e
			WHERE e.knowledge_base_id=$1 AND e.merged_into_id IS NULL AND `+s.currentEvidencePredicate("e")+` AND e.id IN (SELECT id FROM walk)
			ORDER BY evidence_count DESC,e.canonical_name,e.id LIMIT $6`, params.base, entityID, predicate, depth, docID, nodeLimit+1)
	} else {
		nodeRows, err = s.db.Query(ctx, `
			SELECT e.id::text,e.knowledge_base_id::text,e.type,e.canonical_name,e.normalized_name,e.review_status,e.revision,
			       (SELECT count(*) FROM knowledge_evidence ev JOIN knowledge_entity mention ON mention.id=ev.subject_id JOIN knowledge_document_version vv ON vv.id=ev.version_id JOIN knowledge_document dd ON dd.id=vv.document_id WHERE ev.subject_type='entity' AND COALESCE(mention.merged_into_id,mention.id)=e.id AND dd.current_version_id=vv.id AND dd.deleted_at IS NULL AND (ev.extraction_run_id IS NULL OR EXISTS (SELECT 1 FROM knowledge_extraction_run er WHERE er.id=ev.extraction_run_id AND er.version_id=vv.id AND er.is_active)))::int AS evidence_count
			FROM knowledge_entity e
			WHERE e.knowledge_base_id=$1 AND e.merged_into_id IS NULL AND `+s.currentEvidencePredicate("e")+`
			ORDER BY evidence_count DESC,e.canonical_name,e.id LIMIT $2`, params.base, nodeLimit+1)
	}
	if err != nil {
		return GraphResponse{}, internal("failed to load knowledge graph nodes", err)
	}
	defer nodeRows.Close()
	nodes := []Entity{}
	for nodeRows.Next() {
		entity, scanErr := scanEntity(nodeRows)
		if scanErr != nil {
			return GraphResponse{}, internal("failed to read graph node", scanErr)
		}
		nodes = append(nodes, entity)
	}
	if err := nodeRows.Err(); err != nil {
		return GraphResponse{}, internal("failed to load knowledge graph nodes", err)
	}
	truncated := len(nodes) > nodeLimit
	if truncated {
		nodes = nodes[:nodeLimit]
	}
	if len(nodes) == 0 {
		return GraphResponse{Nodes: []Entity{}, Relations: []Relation{}, NodeCount: 0, EdgeCount: 0}, nil
	}
	nodeIDs := make([]pgtype.UUID, len(nodes))
	for i, node := range nodes {
		nodeIDs[i], _ = parseID(node.ID, "entity_id")
	}
	relArgs := []any{params.base, nodeIDs, predicate, docID}
	relRows, err := s.db.Query(ctx, `
		SELECT r.id::text,r.knowledge_base_id::text,r.source_entity_id::text,r.target_entity_id::text,r.predicate,r.qualifier,r.review_status,r.revision,
		       (SELECT count(*) FROM knowledge_evidence ev JOIN knowledge_document_version vv ON vv.id=ev.version_id JOIN knowledge_document dd ON dd.id=vv.document_id WHERE ev.subject_type='relation' AND ev.subject_id=r.id AND dd.current_version_id=vv.id AND dd.deleted_at IS NULL AND (ev.extraction_run_id IS NULL OR EXISTS (SELECT 1 FROM knowledge_extraction_run er WHERE er.id=ev.extraction_run_id AND er.version_id=vv.id AND er.is_active)))::int AS evidence_count
		FROM (
			SELECT r.id,r.knowledge_base_id,
			       COALESCE(source.merged_into_id,source.id) AS source_entity_id,
			       COALESCE(target.merged_into_id,target.id) AS target_entity_id,
			       r.predicate,r.qualifier,r.review_status,r.revision
			FROM knowledge_relation r
			JOIN knowledge_entity source ON source.id=r.source_entity_id
			JOIN knowledge_entity target ON target.id=r.target_entity_id
				WHERE r.knowledge_base_id=$1 AND r.review_status <> 'rejected'
		) r
		WHERE r.source_entity_id=ANY($2) AND r.target_entity_id=ANY($2)
		  AND ($3='' OR r.predicate=$3)
		  AND ($4::uuid='00000000-0000-0000-0000-000000000000' OR EXISTS (SELECT 1 FROM knowledge_evidence ev WHERE ev.subject_type='relation' AND ev.subject_id=r.id AND ev.version_id IN (SELECT current_version_id FROM knowledge_document WHERE id=$4)))
			  AND r.review_status <> 'rejected'
			  AND EXISTS (SELECT 1 FROM knowledge_evidence ev JOIN knowledge_document_version vv ON vv.id=ev.version_id JOIN knowledge_document dd ON dd.id=vv.document_id WHERE ev.subject_type='relation' AND ev.subject_id=r.id AND dd.current_version_id=vv.id AND dd.deleted_at IS NULL AND (ev.extraction_run_id IS NULL OR EXISTS (SELECT 1 FROM knowledge_extraction_run er WHERE er.id=ev.extraction_run_id AND er.version_id=vv.id AND er.is_active)))
		ORDER BY evidence_count DESC,r.id LIMIT $5`, relArgs[0], relArgs[1], relArgs[2], relArgs[3], 401)
	if err != nil {
		return GraphResponse{}, internal("failed to load knowledge graph relations", err)
	}
	defer relRows.Close()
	relations := []Relation{}
	for relRows.Next() {
		relation, scanErr := scanRelation(relRows)
		if scanErr != nil {
			return GraphResponse{}, internal("failed to read graph relation", scanErr)
		}
		relations = append(relations, relation)
	}
	if err := relRows.Err(); err != nil {
		return GraphResponse{}, internal("failed to load knowledge graph relations", err)
	}
	if len(relations) > 400 {
		truncated = true
		relations = relations[:400]
	}
	return GraphResponse{Nodes: nodes, Relations: relations, Truncated: truncated, NodeCount: len(nodes), EdgeCount: len(relations)}, nil
}

func (s *Service) RelationEvidence(ctx context.Context, workspaceID, actorID, baseID, relationID string) ([]Evidence, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return nil, err
	}
	id, err := parseID(relationID, "relation_id")
	if err != nil {
		return nil, err
	}
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_relation WHERE id=$1 AND knowledge_base_id=$2)`, id, params.base).Scan(&exists); err != nil {
		return nil, internal("failed to check graph relation", err)
	}
	if !exists {
		return nil, notFound()
	}
	return s.listEvidenceForSubject(ctx, params.base, "relation", id)
}

type GraphEditInput struct {
	Operation        string
	TargetID         string
	Payload          map[string]any
	ExpectedRevision int64
}

func (s *Service) ApplyGraphEdit(ctx context.Context, workspaceID, actorID, baseID string, input GraphEditInput) (GraphEdit, error) {
	if err := s.checkEnabled(); err != nil {
		return GraphEdit{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return GraphEdit{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return GraphEdit{}, err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return GraphEdit{}, err
	}
	actor, err := parseID(actorID, "user_id")
	if err != nil {
		return GraphEdit{}, err
	}
	target, err := parseID(input.TargetID, "target_id")
	if err != nil {
		return GraphEdit{}, err
	}
	if input.ExpectedRevision <= 0 {
		return GraphEdit{}, badRequest("expected_revision_required", "expected_revision is required")
	}
	operation := strings.TrimSpace(input.Operation)
	if !contains([]string{"rename", "retype", "merge", "reject_relation", "edit_relation", "confirm"}, operation) {
		return GraphEdit{}, badRequest("invalid_graph_operation", "unsupported graph edit operation")
	}
	payload, err := jsonBytes(input.Payload)
	if err != nil {
		return GraphEdit{}, internal("failed to encode graph edit", err)
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return GraphEdit{}, internal("failed to start graph edit transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, params.workspace); err != nil {
		return GraphEdit{}, err
	}
	var lockedBase pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL FOR UPDATE`, params.base, params.workspace).Scan(&lockedBase); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GraphEdit{}, notFound()
		}
		return GraphEdit{}, internal("failed to lock knowledge base for graph edit", err)
	}
	previous := map[string]any{}
	targetKind, _ := input.Payload["target_type"].(string)
	entityOperation := operation == "rename" || operation == "retype" || (operation == "confirm" && targetKind != "relation")
	if operation == "merge" {
		value, ok := input.Payload["target_entity_id"].(string)
		if !ok {
			return GraphEdit{}, badRequest("target_entity_required", "target_entity_id is required")
		}
		survivor, parseErr := parseID(value, "target_entity_id")
		if parseErr != nil {
			return GraphEdit{}, parseErr
		}
		if survivor == target {
			return GraphEdit{}, badRequest("merge_target_same_entity", "an entity cannot be merged into itself")
		}
		// Lock both entities in UUID order. This prevents two reciprocal merge
		// requests from deadlocking while still keeping the merge redirect and
		// alias insertion in one transaction.
		first, second := target, survivor
		if bytes.Compare(first.Bytes[:], second.Bytes[:]) > 0 {
			first, second = second, first
		}
		var sourceType, sourceName, sourceStatus string
		var sourceRevision int64
		var sourceMerged, survivorMerged pgtype.Text
		for _, id := range []pgtype.UUID{first, second} {
			var typ, name, status string
			var revision int64
			var mergedInto pgtype.Text
			if err := tx.QueryRow(ctx, `SELECT type,canonical_name,review_status,revision,merged_into_id::text FROM knowledge_entity WHERE id=$1 AND knowledge_base_id=$2 FOR UPDATE`, id, params.base).Scan(&typ, &name, &status, &revision, &mergedInto); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return GraphEdit{}, notFound()
				}
				return GraphEdit{}, internal("failed to lock merge entity", err)
			}
			if id == target {
				sourceType, sourceName, sourceStatus, sourceRevision, sourceMerged = typ, name, status, revision, mergedInto
			} else {
				survivorMerged = mergedInto
			}
		}
		if sourceMerged.Valid || survivorMerged.Valid {
			return GraphEdit{}, conflict("entity_already_merged", "both merge source and survivor must be canonical entities")
		}
		if sourceRevision != input.ExpectedRevision {
			return GraphEdit{}, conflict("revision_conflict", "graph entity changed; refresh before merging")
		}
		previous = map[string]any{
			"type":               sourceType,
			"canonical_name":     sourceName,
			"review_status":      sourceStatus,
			"merged_into_id":     nil,
			"source_entity_id":   uuidString(target),
			"survivor_entity_id": uuidString(survivor),
		}
		aliasIDs := []string{}
		if normalized := NormalizeAlias(sourceName); normalized != "" {
			aliasID, aliasIDString := newID()
			provenance, _ := jsonBytes(map[string]any{"source": "manual_merge", "source_entity_id": uuidString(target)})
			result, insertErr := tx.Exec(ctx, `INSERT INTO knowledge_entity_alias(id,workspace_id,knowledge_base_id,entity_id,normalized_alias,disambiguator,provenance) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(knowledge_base_id,normalized_alias,disambiguator) DO NOTHING`, aliasID, params.workspace, params.base, survivor, normalized, uuidString(target), provenance)
			if insertErr != nil {
				return GraphEdit{}, internal("failed to store merge alias", insertErr)
			}
			if result.RowsAffected() > 0 {
				aliasIDs = append(aliasIDs, aliasIDString)
			}
		}
		previous["alias_ids"] = aliasIDs
		_, err = tx.Exec(ctx, `UPDATE knowledge_entity SET merged_into_id=$2,review_status='merged',revision=revision+1,updated_at=now() WHERE id=$1 AND knowledge_base_id=$3 AND merged_into_id IS NULL`, target, survivor, params.base)
	} else if entityOperation {
		var typ, name, status string
		var revision int64
		var mergedInto pgtype.Text
		if err := tx.QueryRow(ctx, `SELECT type,canonical_name,review_status,revision,merged_into_id::text FROM knowledge_entity WHERE id=$1 AND knowledge_base_id=$2 FOR UPDATE`, target, params.base).Scan(&typ, &name, &status, &revision, &mergedInto); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return GraphEdit{}, notFound()
			}
			return GraphEdit{}, internal("failed to lock graph entity", err)
		}
		if revision != input.ExpectedRevision {
			return GraphEdit{}, conflict("revision_conflict", "graph entity changed; refresh before editing")
		}
		if mergedInto.Valid {
			return GraphEdit{}, conflict("entity_merged", "edit the canonical survivor entity instead")
		}
		previous = map[string]any{"type": typ, "canonical_name": name, "review_status": status}
		switch operation {
		case "rename":
			value, ok := input.Payload["canonical_name"].(string)
			if !ok || strings.TrimSpace(value) == "" {
				return GraphEdit{}, badRequest("canonical_name_required", "canonical_name is required")
			}
			_, err = tx.Exec(ctx, `UPDATE knowledge_entity SET canonical_name=$2,normalized_name=$3,revision=revision+1,updated_at=now() WHERE id=$1`, target, strings.TrimSpace(value), NormalizeAlias(value))
		case "retype":
			value, ok := input.Payload["type"].(string)
			if !ok || !contains(SupportedEntityTypes, value) {
				return GraphEdit{}, badRequest("invalid_entity_type", "unsupported entity type")
			}
			_, err = tx.Exec(ctx, `UPDATE knowledge_entity SET type=$2,revision=revision+1,updated_at=now() WHERE id=$1`, target, value)
		case "confirm":
			_, err = tx.Exec(ctx, `UPDATE knowledge_entity SET review_status='confirmed',revision=revision+1,updated_at=now() WHERE id=$1`, target)
		}
	} else {
		var predicate, status string
		var qualifier []byte
		var revision int64
		if err := tx.QueryRow(ctx, `SELECT predicate,qualifier,review_status,revision FROM knowledge_relation WHERE id=$1 AND knowledge_base_id=$2 FOR UPDATE`, target, params.base).Scan(&predicate, &qualifier, &status, &revision); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return GraphEdit{}, notFound()
			}
			return GraphEdit{}, internal("failed to lock graph relation", err)
		}
		if revision != input.ExpectedRevision {
			return GraphEdit{}, conflict("revision_conflict", "graph relation changed; refresh before editing")
		}
		previous = map[string]any{"predicate": predicate, "qualifier": mapJSON(qualifier), "review_status": status}
		switch operation {
		case "reject_relation":
			_, err = tx.Exec(ctx, `UPDATE knowledge_relation SET review_status='rejected',revision=revision+1,updated_at=now() WHERE id=$1`, target)
		case "confirm":
			_, err = tx.Exec(ctx, `UPDATE knowledge_relation SET review_status='confirmed',revision=revision+1,updated_at=now() WHERE id=$1`, target)
		case "edit_relation":
			value, ok := input.Payload["predicate"].(string)
			if !ok || !contains(SupportedPredicates, value) {
				return GraphEdit{}, badRequest("invalid_predicate", "unsupported graph predicate")
			}
			qualifierValue := input.Payload["qualifier"]
			qualifierJSON, marshalErr := jsonBytes(qualifierValue)
			if marshalErr != nil {
				return GraphEdit{}, internal("failed to encode relation qualifier", marshalErr)
			}
			_, err = tx.Exec(ctx, `UPDATE knowledge_relation SET predicate=$2,qualifier=$3,revision=revision+1,updated_at=now() WHERE id=$1`, target, value, qualifierJSON)
		}
	}
	if err != nil {
		return GraphEdit{}, internal("failed to apply graph edit", err)
	}
	previousJSON, _ := json.Marshal(previous)
	edit, err := scanGraphEdit(tx.QueryRow(ctx, `INSERT INTO knowledge_graph_edit(workspace_id,knowledge_base_id,operation,target_id,payload,previous_state,actor_id,expected_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text,operation,target_id::text,payload,previous_state,actor_id::text,expected_revision,created_at,reverted_at`, params.workspace, params.base, operation, target, payload, previousJSON, actor, input.ExpectedRevision))
	if err != nil {
		return GraphEdit{}, internal("failed to record graph edit", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return GraphEdit{}, internal("failed to commit graph edit", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	return edit, nil
}

func scanGraphEdit(row pgx.Row) (GraphEdit, error) {
	var edit GraphEdit
	var payload, previous []byte
	var reverted pgtype.Timestamptz
	err := row.Scan(&edit.ID, &edit.Operation, &edit.TargetID, &payload, &previous, &edit.ActorID, &edit.ExpectedRevision, &edit.CreatedAt, &reverted)
	if err != nil {
		return GraphEdit{}, err
	}
	edit.Payload = mapJSON(payload)
	edit.PreviousState = mapJSON(previous)
	edit.RevertedAt = nullableTime(reverted)
	return edit, nil
}

func (s *Service) ListGraphEdits(ctx context.Context, workspaceID, actorID, baseID string, limit int) ([]GraphEdit, error) {
	if err := s.checkEnabled(); err != nil {
		return nil, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(ctx, `SELECT id::text,operation,target_id::text,payload,previous_state,actor_id::text,expected_revision,created_at,reverted_at FROM knowledge_graph_edit WHERE knowledge_base_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, params.base, limit)
	if err != nil {
		return nil, internal("failed to list graph edits", err)
	}
	defer rows.Close()
	items := []GraphEdit{}
	for rows.Next() {
		item, scanErr := scanGraphEdit(rows)
		if scanErr != nil {
			return nil, internal("failed to read graph edit", scanErr)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) RevertGraphEdit(ctx context.Context, workspaceID, actorID, baseID, editID string) (GraphEdit, error) {
	if err := s.checkEnabled(); err != nil {
		return GraphEdit{}, err
	}
	params, err := s.readableBaseParams(workspaceID, actorID, baseID)
	if err != nil {
		return GraphEdit{}, err
	}
	if _, err := s.GetBase(ctx, workspaceID, actorID, baseID); err != nil {
		return GraphEdit{}, err
	}
	if err := s.ensureBaseManager(ctx, workspaceID, actorID, baseID); err != nil {
		return GraphEdit{}, err
	}
	id, err := parseID(editID, "edit_id")
	if err != nil {
		return GraphEdit{}, err
	}
	tx, err := s.txStarter.Begin(ctx)
	if err != nil {
		return GraphEdit{}, internal("failed to start graph revert transaction", err)
	}
	defer tx.Rollback(ctx)
	if err := lockKnowledgeWorkspace(ctx, tx, params.workspace); err != nil {
		return GraphEdit{}, err
	}
	var lockedBase pgtype.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_base WHERE id=$1 AND workspace_id=$2 AND deleted_at IS NULL FOR UPDATE`, params.base, params.workspace).Scan(&lockedBase); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GraphEdit{}, notFound()
		}
		return GraphEdit{}, internal("failed to lock knowledge base for graph revert", err)
	}
	edit, err := scanGraphEdit(tx.QueryRow(ctx, `SELECT id::text,operation,target_id::text,payload,previous_state,actor_id::text,expected_revision,created_at,reverted_at FROM knowledge_graph_edit WHERE id=$1 AND knowledge_base_id=$2 FOR UPDATE`, id, params.base))
	if errors.Is(err, pgx.ErrNoRows) {
		return GraphEdit{}, notFound()
	}
	if err != nil {
		return GraphEdit{}, internal("failed to load graph edit", err)
	}
	if edit.RevertedAt != nil {
		return edit, nil
	}
	var dependent int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM knowledge_graph_edit WHERE knowledge_base_id=$1 AND target_id=$2 AND created_at>$3 AND reverted_at IS NULL`, params.base, edit.TargetID, edit.CreatedAt).Scan(&dependent); err != nil {
		return GraphEdit{}, internal("failed to check graph edit dependencies", err)
	}
	if dependent > 0 {
		return GraphEdit{}, conflict("graph_edit_has_dependents", "later graph edits must be reverted first")
	}
	target, err := parseID(edit.TargetID, "target_id")
	if err != nil {
		return GraphEdit{}, err
	}
	previous := edit.PreviousState
	relationOperation := edit.Operation == "reject_relation" || edit.Operation == "edit_relation" || (edit.Operation == "confirm" && previous["predicate"] != nil)
	if edit.Operation != "merge" {
		expectedCurrentRevision := edit.ExpectedRevision + 1
		if relationOperation {
			var currentRevision int64
			if err := tx.QueryRow(ctx, `SELECT revision FROM knowledge_relation WHERE id=$1 AND knowledge_base_id=$2 FOR UPDATE`, target, params.base).Scan(&currentRevision); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return GraphEdit{}, notFound()
				}
				return GraphEdit{}, internal("failed to lock graph relation for revert", err)
			}
			if currentRevision != expectedCurrentRevision {
				return GraphEdit{}, conflict("graph_edit_state_changed", "the graph relation changed after this edit; refresh before reverting")
			}
		} else {
			var currentRevision int64
			var mergedInto pgtype.Text
			if err := tx.QueryRow(ctx, `SELECT revision,merged_into_id::text FROM knowledge_entity WHERE id=$1 AND knowledge_base_id=$2 FOR UPDATE`, target, params.base).Scan(&currentRevision, &mergedInto); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return GraphEdit{}, notFound()
				}
				return GraphEdit{}, internal("failed to lock graph entity for revert", err)
			}
			if currentRevision != expectedCurrentRevision || mergedInto.Valid {
				return GraphEdit{}, conflict("graph_edit_state_changed", "the graph entity changed after this edit; refresh before reverting")
			}
		}
	}
	switch edit.Operation {
	case "rename":
		name, _ := previous["canonical_name"].(string)
		_, err = tx.Exec(ctx, `UPDATE knowledge_entity SET canonical_name=$2,normalized_name=$3,revision=revision+1,updated_at=now() WHERE id=$1 AND knowledge_base_id=$4 AND revision=$5`, target, name, NormalizeAlias(name), params.base, edit.ExpectedRevision+1)
	case "retype":
		typ, _ := previous["type"].(string)
		_, err = tx.Exec(ctx, `UPDATE knowledge_entity SET type=$2,revision=revision+1,updated_at=now() WHERE id=$1 AND knowledge_base_id=$3 AND revision=$4`, target, typ, params.base, edit.ExpectedRevision+1)
	case "confirm":
		status, _ := previous["review_status"].(string)
		if relationOperation {
			_, err = tx.Exec(ctx, `UPDATE knowledge_relation SET review_status=$2,revision=revision+1,updated_at=now() WHERE id=$1 AND knowledge_base_id=$3 AND revision=$4`, target, status, params.base, edit.ExpectedRevision+1)
		} else {
			_, err = tx.Exec(ctx, `UPDATE knowledge_entity SET review_status=$2,revision=revision+1,updated_at=now() WHERE id=$1 AND knowledge_base_id=$3 AND revision=$4`, target, status, params.base, edit.ExpectedRevision+1)
		}
	case "reject_relation", "edit_relation":
		// Relation edits are reverted with the same fields saved in previous_state.
		predicate, _ := previous["predicate"].(string)
		qualifier, _ := jsonBytes(previous["qualifier"])
		status, _ := previous["review_status"].(string)
		_, err = tx.Exec(ctx, `UPDATE knowledge_relation SET predicate=$2,qualifier=$3,review_status=$4,revision=revision+1,updated_at=now() WHERE id=$1 AND knowledge_base_id=$5 AND revision=$6`, target, predicate, qualifier, status, params.base, edit.ExpectedRevision+1)
	case "merge":
		status, _ := previous["review_status"].(string)
		sourceEntityID, _ := previous["source_entity_id"].(string)
		survivorEntityID, _ := previous["survivor_entity_id"].(string)
		if sourceEntityID == "" {
			sourceEntityID = edit.TargetID
		}
		source, parseSourceErr := parseID(sourceEntityID, "source_entity_id")
		if parseSourceErr != nil {
			return GraphEdit{}, parseSourceErr
		}
		survivor, parseSurvivorErr := parseID(survivorEntityID, "survivor_entity_id")
		if parseSurvivorErr != nil {
			return GraphEdit{}, parseSurvivorErr
		}
		first, second := source, survivor
		if bytes.Compare(first.Bytes[:], second.Bytes[:]) > 0 {
			first, second = second, first
		}
		for _, entityID := range []pgtype.UUID{first, second} {
			var lockedID pgtype.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM knowledge_entity WHERE id=$1 AND knowledge_base_id=$2 FOR UPDATE`, entityID, params.base).Scan(&lockedID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return GraphEdit{}, notFound()
				}
				return GraphEdit{}, internal("failed to lock merge entities for revert", err)
			}
		}
		result, updateErr := tx.Exec(ctx, `UPDATE knowledge_entity SET merged_into_id=NULL,review_status=$2,revision=revision+1,updated_at=now() WHERE id=$1 AND knowledge_base_id=$3 AND merged_into_id=$4 AND revision=$5`, source, status, params.base, survivor, edit.ExpectedRevision+1)
		if updateErr != nil {
			err = updateErr
		} else if result.RowsAffected() == 0 {
			err = conflict("merge_state_changed", "the merge target changed; refresh before reverting")
		} else {
			if rawAliasIDs, ok := previous["alias_ids"].([]any); ok {
				for _, value := range rawAliasIDs {
					aliasID, ok := value.(string)
					if !ok || strings.TrimSpace(aliasID) == "" {
						continue
					}
					alias, aliasErr := parseID(aliasID, "alias_id")
					if aliasErr != nil {
						err = aliasErr
						break
					}
					if _, err = tx.Exec(ctx, `DELETE FROM knowledge_entity_alias WHERE id=$1 AND knowledge_base_id=$2 AND entity_id=$3`, alias, params.base, survivor); err != nil {
						break
					}
				}
			}
		}
	}
	if err != nil {
		return GraphEdit{}, internal("failed to revert graph edit", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE knowledge_graph_edit SET reverted_at=now() WHERE id=$1 AND knowledge_base_id=$2`, id, params.base); err != nil {
		return GraphEdit{}, internal("failed to mark graph edit reverted", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return GraphEdit{}, internal("failed to commit graph revert", err)
	}
	s.notifyKnowledgeInvalidation(ctx, workspaceID, baseID, actorID)
	edit.RevertedAt = func() *time.Time { now := s.now(); return &now }()
	return edit, nil
}
