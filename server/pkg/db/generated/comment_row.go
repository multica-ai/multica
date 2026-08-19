package db

// Comment converts the sqlc row returned by the CTE-backed create query into
// the canonical model used by comment rendering and task side effects. The
// row's IssueRevision remains available separately for owner-cache coherence.
func (r CreateCommentRow) Comment() Comment {
	return Comment{
		ID:             r.ID,
		IssueID:        r.IssueID,
		AuthorType:     r.AuthorType,
		AuthorID:       r.AuthorID,
		Content:        r.Content,
		Type:           r.Type,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
		ParentID:       r.ParentID,
		WorkspaceID:    r.WorkspaceID,
		ResolvedAt:     r.ResolvedAt,
		ResolvedByType: r.ResolvedByType,
		ResolvedByID:   r.ResolvedByID,
		SourceTaskID:   r.SourceTaskID,
		QuickActionID:  r.QuickActionID,
		ViaPluginID:    r.ViaPluginID,
		Revision:       r.Revision,
	}
}
