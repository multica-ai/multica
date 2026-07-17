package operatingsystem

// Terminology keeps customer-facing labels configurable without changing the
// canonical API and database names. Defaults stay vendor-neutral; profiles
// with branded methodology names are user data, never code.
type Terminology struct {
	Strategy    string `json:"strategy"`
	Rock        string `json:"rock"`
	Rocks       string `json:"rocks"`
	VisionPlan  string `json:"vision_plan"`
	Meetings    string `json:"meetings"`
	OrgChart    string `json:"org_chart"`
	Scorecard   string `json:"scorecard"`
	IssuesList  string `json:"issues_list"`
	StrategyMap string `json:"strategy_map"`
}

// OsElementResponse reports one Operating System element with the workspace
// toggle applied and the registry default alongside it.
type OsElementResponse struct {
	Key            string `json:"key"`
	Enabled        bool   `json:"enabled"`
	DefaultEnabled bool   `json:"default_enabled"`
}

type GoalTypeInput struct {
	Name       string `json:"name"`
	Color      string `json:"color,omitempty"`
	ScopeLabel string `json:"scope_label,omitempty"`
	Position   int32  `json:"position"`
}

type GoalTypeResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	ScopeLabel  string `json:"scope_label"`
	Position    int32  `json:"position"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// OperatingPeriodInput creates or updates a period. Month and quarter periods
// snap to calendar boundaries and may omit name and ends_on; custom periods
// provide all three.
type OperatingPeriodInput struct {
	Name     string `json:"name,omitempty"`
	Unit     string `json:"unit"`
	StartsOn string `json:"starts_on"`
	EndsOn   string `json:"ends_on,omitempty"`
}

type StrategyItemInput struct {
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	HorizonUnit  string `json:"horizon_unit,omitempty"`
	HorizonCount int32  `json:"horizon_count,omitempty"`
	HorizonLabel string `json:"horizon_label,omitempty"`
	Position     int32  `json:"position"`
	State        string `json:"state,omitempty"`
}

type RockInput struct {
	Title          string   `json:"title,omitempty"`
	Description    string   `json:"description,omitempty"`
	OwnerType      string   `json:"owner_type,omitempty"`
	OwnerID        string   `json:"owner_id,omitempty"`
	PeriodID       string   `json:"period_id,omitempty"`
	GoalTypeID     string   `json:"goal_type_id,omitempty"`
	Confidence     int32    `json:"confidence"`
	ReportedHealth string   `json:"reported_health"`
	ProjectIDs     []string `json:"project_ids,omitempty"`
	IssueIDs       []string `json:"issue_ids,omitempty"`
	StrategyItemID string   `json:"strategy_item_id,omitempty"`
	// Legacy project-backed clients remain accepted at the API boundary.
	ProjectID   string `json:"project_id,omitempty"`
	PeriodStart string `json:"period_start,omitempty"`
	PeriodEnd   string `json:"period_end,omitempty"`
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
	HorizonLabel string `json:"horizon_label,omitempty"`
	Position     int32  `json:"position"`
	State        string `json:"state"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type StrategyHistoryResponse struct {
	ID             string         `json:"id"`
	StrategyItemID string         `json:"strategy_item_id"`
	Action         string         `json:"action"`
	Title          string         `json:"title"`
	Snapshot       map[string]any `json:"snapshot"`
	ChangedAt      string         `json:"changed_at"`
}

type RockResponse struct {
	ID                 string        `json:"id"`
	Title              string        `json:"title"`
	Description        string        `json:"description,omitempty"`
	OwnerType          string        `json:"owner_type,omitempty"`
	OwnerID            string        `json:"owner_id,omitempty"`
	OwnerName          string        `json:"owner_name,omitempty"`
	PeriodID           string        `json:"period_id"`
	PeriodName         string        `json:"period_name"`
	GoalTypeID         string        `json:"goal_type_id,omitempty"`
	GoalTypeName       string        `json:"goal_type_name,omitempty"`
	GoalTypeColor      string        `json:"goal_type_color,omitempty"`
	GoalTypeScopeLabel string        `json:"goal_type_scope_label,omitempty"`
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
	ProjectCount       int32         `json:"project_count"`
	HealthScore        int32         `json:"health_score"`
	StrategyItemID     string        `json:"strategy_item_id,omitempty"`
	StrategyItemTitle  string        `json:"strategy_item_title,omitempty"`
	Projects           []RockProject `json:"projects"`
	Issues             []RockIssue   `json:"issues"`
	CheckIns           []RockCheckIn `json:"check_ins"`
	CreatedAt          string        `json:"created_at"`
	UpdatedAt          string        `json:"updated_at"`
}

type OperatingPeriodResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Unit        string `json:"unit"`
	StartsOn    string `json:"starts_on"`
	EndsOn      string `json:"ends_on"`
}

type RockProject struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	IssueCount     int32  `json:"issue_count"`
	DoneIssueCount int32  `json:"done_issue_count"`
}

type RockIssue struct {
	ID           string `json:"id"`
	Identifier   string `json:"identifier"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	ProjectID    string `json:"project_id,omitempty"`
	ProjectTitle string `json:"project_title,omitempty"`
}

type RockCheckInInput struct {
	Confidence     int32  `json:"confidence"`
	ReportedHealth string `json:"reported_health"`
	Note           string `json:"note,omitempty"`
}

type RockCheckIn struct {
	ID             string `json:"id"`
	Confidence     int32  `json:"confidence"`
	ReportedHealth string `json:"reported_health"`
	Note           string `json:"note"`
	CreatedByType  string `json:"created_by_type"`
	CreatedByID    string `json:"created_by_id"`
	CreatedAt      string `json:"created_at"`
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
