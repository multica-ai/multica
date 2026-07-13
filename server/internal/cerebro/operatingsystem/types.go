package operatingsystem

// Terminology keeps customer-facing labels configurable without changing the
// canonical API and database names.
type Terminology struct {
	Strategy string `json:"strategy"`
	Rock     string `json:"rock"`
	Rocks    string `json:"rocks"`
}

type StrategyItemInput struct {
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	HorizonUnit  string `json:"horizon_unit,omitempty"`
	HorizonCount int32  `json:"horizon_count,omitempty"`
	Position     int32  `json:"position"`
	State        string `json:"state,omitempty"`
}

type RockInput struct {
	ProjectID      string `json:"project_id"`
	PeriodStart    string `json:"period_start"`
	PeriodEnd      string `json:"period_end"`
	Confidence     int32  `json:"confidence"`
	ReportedHealth string `json:"reported_health"`
}

type DerivedHealth struct {
	State        string `json:"state"`
	Reason       string `json:"reason"`
	CalculatedAt string `json:"calculated_at"`
}

type SettingsResponse struct {
	WorkspaceID string      `json:"workspace_id"`
	Terminology Terminology `json:"terminology"`
	CreatedAt   string      `json:"created_at,omitempty"`
	UpdatedAt   string      `json:"updated_at,omitempty"`
}

type StrategyItemResponse struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	HorizonUnit  string `json:"horizon_unit,omitempty"`
	HorizonCount int32  `json:"horizon_count,omitempty"`
	Position     int32  `json:"position"`
	State        string `json:"state"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type RockResponse struct {
	ProjectID          string        `json:"project_id"`
	WorkspaceID        string        `json:"workspace_id"`
	ProjectTitle       string        `json:"project_title"`
	ProjectDescription string        `json:"project_description,omitempty"`
	ProjectStatus      string        `json:"project_status"`
	LeadType           string        `json:"lead_type,omitempty"`
	LeadID             string        `json:"lead_id,omitempty"`
	PeriodStart        string        `json:"period_start"`
	PeriodEnd          string        `json:"period_end"`
	Confidence         int32         `json:"confidence"`
	ReportedHealth     string        `json:"reported_health"`
	DerivedHealth      DerivedHealth `json:"derived_health"`
	IssueCount         int32         `json:"issue_count"`
	DoneIssueCount     int32         `json:"done_issue_count"`
	BlockedIssueCount  int32         `json:"blocked_issue_count"`
	CreatedAt          string        `json:"created_at"`
	UpdatedAt          string        `json:"updated_at"`
}

type ObjectConnectionInput struct {
	SourceType       string `json:"source_type"`
	SourceID         string `json:"source_id"`
	TargetType       string `json:"target_type"`
	TargetID         string `json:"target_id"`
	RelationshipType string `json:"relationship_type,omitempty"`
	Provenance       string `json:"provenance,omitempty"`
}

type ObjectConnectionResponse struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	SourceType       string `json:"source_type"`
	SourceID         string `json:"source_id"`
	TargetType       string `json:"target_type"`
	TargetID         string `json:"target_id"`
	RelationshipType string `json:"relationship_type"`
	Provenance       string `json:"provenance"`
	CreatedByType    string `json:"created_by_type"`
	CreatedByID      string `json:"created_by_id"`
	CreatedAt        string `json:"created_at"`
}
