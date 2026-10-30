package db

// Attachment converts the sqlc row returned by the CTE-backed create query
// into the canonical model used by the rest of the application.
func (r CreateAttachmentRow) Attachment() Attachment {
	return Attachment{
		ID:            r.ID,
		WorkspaceID:   r.WorkspaceID,
		IssueID:       r.IssueID,
		CommentID:     r.CommentID,
		UploaderType:  r.UploaderType,
		UploaderID:    r.UploaderID,
		Filename:      r.Filename,
		Url:           r.Url,
		ContentType:   r.ContentType,
		SizeBytes:     r.SizeBytes,
		CreatedAt:     r.CreatedAt,
		ChatSessionID: r.ChatSessionID,
		ChatMessageID: r.ChatMessageID,
		TaskID:        r.TaskID,
	}
}
