-- Issue-Document linking

-- name: LinkIssueDocument :exec
INSERT INTO issue_document_link (issue_id, document_id, link_type)
VALUES ($1, $2, $3)
ON CONFLICT (issue_id, document_id) DO UPDATE SET link_type = $3;

-- name: UnlinkIssueDocument :exec
DELETE FROM issue_document_link WHERE issue_id = $1 AND document_id = $2;

-- name: ListLinkedDocumentsForIssue :many
SELECT d.* FROM workspace_document d
JOIN issue_document_link l ON l.document_id = d.id
WHERE l.issue_id = $1 AND d.archived_at IS NULL;
