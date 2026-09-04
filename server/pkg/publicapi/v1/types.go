package publicapiv1

// Context is Plugin-specific bootstrap data. It lives in the versioned
// contract package so service-layer fields cannot leak into the wire format.
type Context struct {
	Workspace         ContextWorkspace `json:"workspace"`
	User              *ContextUser     `json:"user,omitempty"`
	Issue             *ContextIssue    `json:"issue,omitempty"`
	Config            map[string]any   `json:"config"`
	GrantedNetDomains []string         `json:"granted_net_domains"`
	Actor             string           `json:"actor"`
}

type ContextWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type ContextUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ContextIssue struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

// Issue intentionally mirrors the fields already exposed by Plugin API v1,
// but is independent from handler.IssueResponse. Adding an App API field can
// therefore no longer widen the public contract by accident.
type Issue struct {
	ID             string         `json:"id"`
	WorkspaceID    string         `json:"workspace_id"`
	Number         int32          `json:"number"`
	Identifier     string         `json:"identifier"`
	Title          string         `json:"title"`
	Description    *string        `json:"description"`
	Status         string         `json:"status"`
	StatusCategory string         `json:"status_category,omitempty"`
	Priority       string         `json:"priority"`
	AssigneeType   *string        `json:"assignee_type"`
	AssigneeID     *string        `json:"assignee_id"`
	CreatorType    string         `json:"creator_type"`
	CreatorID      string         `json:"creator_id"`
	ParentIssueID  *string        `json:"parent_issue_id"`
	ProjectID      *string        `json:"project_id"`
	Position       float64        `json:"position"`
	Stage          *int32         `json:"stage"`
	StartDate      *string        `json:"start_date"`
	DueDate        *string        `json:"due_date"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	Revision       int64          `json:"revision"`
	LastActivityAt *string        `json:"last_activity_at"`
	Metadata       map[string]any `json:"metadata"`
	Properties     map[string]any `json:"properties"`
}

type PatchIssueRequest struct {
	ExpectedRevision *int64  `json:"expected_revision,omitempty"`
	Title            *string `json:"title,omitempty"`
	Description      *string `json:"description,omitempty"`
}

type Comment struct {
	ID         string `json:"id"`
	AuthorType string `json:"author_type"`
	AuthorID   string `json:"author_id"`
	Content    string `json:"content"`
	Type       string `json:"type"`
	ParentID   string `json:"parent_id,omitempty"`
	CreatedAt  string `json:"created_at"`
}

type CommentListResponse struct {
	Comments []Comment `json:"comments"`
}

type CreateCommentRequest struct {
	Content  string  `json:"content"`
	ParentID *string `json:"parent_id,omitempty"`
}

type StorageKey struct {
	Key       string `json:"key"`
	SizeBytes int64  `json:"size_bytes"`
	UpdatedAt string `json:"updated_at"`
}

type StorageKeyListResponse struct {
	Keys []StorageKey `json:"keys"`
}

type StorageValueResponse struct {
	Value string `json:"value"`
}

type PutStorageValueRequest struct {
	Value string `json:"value"`
}
