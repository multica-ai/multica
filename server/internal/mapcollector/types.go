package mapcollector

import "encoding/json"

const MappingVersion = "NEW31-MAP-v1"

type Contract struct {
	MappingVersion  string              `json:"mapping_version"`
	SnapshotLabel   string              `json:"snapshot_label"`
	PostgresVersion string              `json:"postgres_version"`
	Schema          string              `json:"schema"`
	Tables          []TableContract     `json:"tables"`
	References      []ReferenceContract `json:"references"`
	Owners          *OwnerContract      `json:"owners,omitempty"`
	Permissions     *PermissionContract `json:"permissions,omitempty"`
	Attachments     *AttachmentContract `json:"attachments,omitempty"`
	Usage           *UsageContract      `json:"usage,omitempty"`
	Tasks           *TaskContract       `json:"tasks,omitempty"`
}

type TableContract struct {
	Name          string           `json:"name"`
	Domain        string           `json:"domain"`
	IDField       string           `json:"id_field"`
	Fields        []FieldContract  `json:"fields"`
	DeniedColumns []string         `json:"denied_columns"`
	Unique        []UniqueContract `json:"unique,omitempty"`
}

type FieldContract struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Nullable bool     `json:"nullable"`
	Role     string   `json:"role,omitempty"`
	Enum     []string `json:"enum,omitempty"`
}

type UniqueContract struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
}

type ReferenceContract struct {
	Name               string   `json:"name"`
	Domain             string   `json:"domain"`
	FromTable          string   `json:"from_table"`
	FromFields         []string `json:"from_fields"`
	ToTable            string   `json:"to_table"`
	ToFields           []string `json:"to_fields"`
	AllowNull          bool     `json:"allow_null"`
	Acyclic            bool     `json:"acyclic,omitempty"`
	FromWorkspaceField string   `json:"from_workspace_field,omitempty"`
	ToWorkspaceField   string   `json:"to_workspace_field,omitempty"`
}

type PermissionContract struct {
	AgentTable           string   `json:"agent_table"`
	AgentIDField         string   `json:"agent_id_field"`
	AgentWorkspaceField  string   `json:"agent_workspace_field"`
	ModeField            string   `json:"mode_field"`
	PrivateMode          string   `json:"private_mode"`
	PublicMode           string   `json:"public_mode"`
	TargetTable          string   `json:"target_table"`
	TargetAgentField     string   `json:"target_agent_field"`
	TargetTypeField      string   `json:"target_type_field"`
	TargetIDField        string   `json:"target_id_field"`
	ScopeField           string   `json:"scope_field"`
	ActionField          string   `json:"action_field"`
	InheritanceField     string   `json:"inheritance_field"`
	AllowedTargets       []string `json:"allowed_targets"`
	WorkspaceTargetType  string   `json:"workspace_target_type"`
	MemberTargetType     string   `json:"member_target_type"`
	MemberTable          string   `json:"member_table"`
	MemberIDField        string   `json:"member_id_field"`
	MemberWorkspaceField string   `json:"member_workspace_field"`
}

type OwnerContract struct {
	WorkspaceTable       string `json:"workspace_table"`
	WorkspaceIDField     string `json:"workspace_id_field"`
	MemberTable          string `json:"member_table"`
	MemberWorkspaceField string `json:"member_workspace_field"`
	MemberRoleField      string `json:"member_role_field"`
	OwnerRole            string `json:"owner_role"`
}

type AttachmentContract struct {
	Table            string `json:"table"`
	StorageKeyField  string `json:"storage_key_field"`
	StorageTypeField string `json:"storage_type_field"`
	SizeField        string `json:"size_field"`
	SHA256Field      string `json:"sha256_field"`
}

type UsageContract struct {
	Table      string            `json:"table"`
	TaskField  string            `json:"task_field"`
	UnitFields map[string]string `json:"unit_fields"`
}

type TaskContract struct {
	Table            string   `json:"table"`
	StatusField      string   `json:"status_field"`
	TerminalStatuses []string `json:"terminal_statuses"`
	OriginatorField  string   `json:"originator_field,omitempty"`
	AccountableField string   `json:"accountable_field,omitempty"`
}

type Report struct {
	MappingVersion  string            `json:"mapping_version"`
	SnapshotIDHash  string            `json:"snapshot_id_hash"`
	PostgresVersion string            `json:"postgres_version"`
	Schema          string            `json:"schema"`
	ContractSHA256  string            `json:"contract_sha256"`
	Domains         []DomainReport    `json:"domains"`
	Tables          []TableReport     `json:"tables"`
	References      []ReferenceReport `json:"references"`
	Permissions     *PermissionReport `json:"permissions,omitempty"`
	Owners          *OwnerReport      `json:"owners,omitempty"`
	Attachments     *AttachmentReport `json:"attachments,omitempty"`
	Usage           *UsageReport      `json:"usage,omitempty"`
	Tasks           *TaskReport       `json:"tasks,omitempty"`
	Rejections      []Rejection       `json:"rejections"`
	Accepted        bool              `json:"accepted"`
}

type TableReport struct {
	Domain        string                    `json:"domain"`
	Name          string                    `json:"name"`
	RowCount      int                       `json:"row_count"`
	Fields        []FieldReport             `json:"fields"`
	DeniedColumns []string                  `json:"denied_columns"`
	EnumCoverage  map[string]map[string]int `json:"enum_coverage,omitempty"`
	Buckets       []BucketReport            `json:"buckets"`
}

type DomainReport struct {
	Name     string         `json:"name"`
	RowCount int            `json:"row_count"`
	Buckets  []BucketReport `json:"buckets"`
}

type FieldReport struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Role     string `json:"role,omitempty"`
}

type BucketReport struct {
	Bucket  int    `json:"bucket"`
	Count   int    `json:"count"`
	HMAC256 string `json:"hmac_sha256"`
}

type ReferenceReport struct {
	Name                string `json:"name"`
	Domain              string `json:"domain"`
	RowsChecked         int    `json:"rows_checked"`
	NullCount           int    `json:"null_count"`
	OrphanCount         int    `json:"orphan_count"`
	CrossWorkspaceCount int    `json:"cross_workspace_count"`
	CycleCount          int    `json:"cycle_count"`
}

type PermissionReport struct {
	PrivateCount             int            `json:"private_count"`
	PublicToCount            int            `json:"public_to_count"`
	TargetTypeCounts         map[string]int `json:"target_type_counts"`
	PrivateWithTargetCount   int            `json:"private_with_target_count"`
	PublicWithoutTargetCount int            `json:"public_without_target_count"`
	InvalidTargetCount       int            `json:"invalid_target_count"`
	ScopeCounts              map[string]int `json:"scope_counts"`
	ActionCounts             map[string]int `json:"action_counts"`
	InheritanceCounts        map[string]int `json:"inheritance_counts"`
}

type OwnerReport struct {
	WorkspacesChecked int `json:"workspaces_checked"`
	MissingOwnerCount int `json:"missing_owner_count"`
}

type AttachmentReport struct {
	RowsChecked       int            `json:"rows_checked"`
	StorageTypeCounts map[string]int `json:"storage_type_counts"`
	TotalBytes        int64          `json:"total_bytes"`
	MissingCount      int            `json:"missing_count"`
	HashMismatchCount int            `json:"hash_mismatch_count"`
	SizeMismatchCount int            `json:"size_mismatch_count"`
}

type UsageReport struct {
	RowsChecked int               `json:"rows_checked"`
	UnitFields  map[string]string `json:"unit_fields"`
	Totals      map[string]string `json:"totals"`
}

type TaskReport struct {
	RowsChecked             int            `json:"rows_checked"`
	StatusCounts            map[string]int `json:"status_counts"`
	NonterminalCount        int            `json:"nonterminal_count"`
	AttributionInvalidCount int            `json:"attribution_invalid_count"`
}

type Rejection struct {
	MappingVersion string   `json:"mapping_version"`
	SnapshotIDHash string   `json:"snapshot_id_hash"`
	Domain         string   `json:"domain"`
	EntityType     string   `json:"entity_type"`
	AnonymousID    string   `json:"anonymous_id,omitempty"`
	ReasonCode     string   `json:"reason_code"`
	Severity       string   `json:"severity"`
	PlannedAction  string   `json:"planned_action"`
	DependencyIDs  []string `json:"anonymous_dependency_ids"`
	BatchID        string   `json:"batch_id"`
	Retryable      bool     `json:"retryable"`
	EvidenceHash   string   `json:"evidence_hash"`
}

type record struct {
	table       *TableContract
	values      map[string]any
	anonymousID string
	bucket      int
	rowHMAC     []byte
}

func (r *Report) Marshal() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
