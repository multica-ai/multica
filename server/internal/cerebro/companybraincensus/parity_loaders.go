package companybraincensus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// FrozenCensusSnapshotLoader reads one checksum-pinned JSON snapshot. It never
// regenerates migration evidence or reads live Connections.
type FrozenCensusSnapshotLoader struct {
	raw            []byte
	expectedSHA256 string
}

var _ FrozenCensusLoader = (*FrozenCensusSnapshotLoader)(nil)

func NewFrozenCensusSnapshotLoader(
	raw []byte,
	expectedSHA256 string,
) *FrozenCensusSnapshotLoader {
	return &FrozenCensusSnapshotLoader{
		raw:            append([]byte(nil), raw...),
		expectedSHA256: expectedSHA256,
	}
}

func (l *FrozenCensusSnapshotLoader) LoadFrozenCensus(
	ctx context.Context,
	workspaceID string,
) (FrozenCensus, error) {
	if err := ctx.Err(); err != nil {
		return FrozenCensus{}, err
	}
	if l == nil || len(l.raw) == 0 {
		return FrozenCensus{}, fmt.Errorf("frozen census snapshot is required")
	}
	if !sha256Hex.MatchString(l.expectedSHA256) {
		return FrozenCensus{}, fmt.Errorf("frozen census snapshot checksum is invalid")
	}
	sum := sha256.Sum256(l.raw)
	if hex.EncodeToString(sum[:]) != l.expectedSHA256 {
		return FrozenCensus{}, fmt.Errorf("frozen census snapshot checksum mismatch")
	}

	requestedWorkspaceID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return FrozenCensus{}, fmt.Errorf("invalid frozen census workspace identity: %w", err)
	}
	var snapshot struct {
		WorkspaceID              string `json:"workspace_id"`
		CensusVersion            int64  `json:"census_version"`
		CompanyBrainConnectionID string `json:"company_brain_connection_id"`
		Report                   Report `json:"report"`
	}
	decoder := json.NewDecoder(bytes.NewReader(l.raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return FrozenCensus{}, fmt.Errorf("decode frozen census snapshot: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return FrozenCensus{}, err
	}

	snapshotWorkspaceID, err := util.ParseUUID(snapshot.WorkspaceID)
	if err != nil || snapshotWorkspaceID != requestedWorkspaceID {
		return FrozenCensus{}, fmt.Errorf("frozen census snapshot workspace mismatch")
	}
	if snapshot.CensusVersion <= 0 {
		return FrozenCensus{}, fmt.Errorf("frozen census snapshot version must be positive")
	}
	if _, err := util.ParseUUID(snapshot.CompanyBrainConnectionID); err != nil {
		return FrozenCensus{}, fmt.Errorf("invalid frozen census logical connection identity: %w", err)
	}
	if snapshot.Report.GeneratedAt.IsZero() {
		return FrozenCensus{}, fmt.Errorf("frozen census snapshot generated time is required")
	}
	return FrozenCensus{
		Report:                   snapshot.Report,
		Version:                  snapshot.CensusVersion,
		CompanyBrainConnectionID: snapshot.CompanyBrainConnectionID,
	}, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("frozen census snapshot contains trailing JSON")
		}
		return fmt.Errorf("decode frozen census snapshot trailing data: %w", err)
	}
	return nil
}

type targetPermissionQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// DatabaseCurrentTargetPermissionLoader combines the current source-scoped
// permission rows with the canonical per-tool policy verdict for each frozen
// source. It performs reads only.
type DatabaseCurrentTargetPermissionLoader struct {
	db     targetPermissionQueryer
	policy connectionPolicy
}

var _ CurrentTargetPermissionLoader = (*DatabaseCurrentTargetPermissionLoader)(nil)

func NewDatabaseCurrentTargetPermissionLoader(
	db targetPermissionQueryer,
	policy connectionPolicy,
) *DatabaseCurrentTargetPermissionLoader {
	return &DatabaseCurrentTargetPermissionLoader{db: db, policy: policy}
}

func (l *DatabaseCurrentTargetPermissionLoader) LoadCurrentTargetPermissions(
	ctx context.Context,
	workspaceID string,
	companyBrainConnectionID string,
	report Report,
) ([]TargetPermission, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if l == nil || l.db == nil {
		return nil, fmt.Errorf("current target permission database is required")
	}
	if l.policy == nil {
		return nil, fmt.Errorf("current target permission policy resolver is required")
	}
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid target permission workspace identity: %w", err)
	}
	connectionUUID, err := util.ParseUUID(companyBrainConnectionID)
	if err != nil {
		return nil, fmt.Errorf("invalid target permission logical connection identity: %w", err)
	}
	evidenceSources, err := targetEvidenceSources(report)
	if err != nil {
		return nil, err
	}

	rows, err := l.db.Query(
		ctx,
		currentTargetPermissionsSQL,
		workspaceUUID,
		connectionUUID,
	)
	if err != nil {
		return nil, fmt.Errorf("query current Company Brain target permissions: %w", err)
	}
	defer rows.Close()

	var out []TargetPermission
	for rows.Next() {
		var (
			permissionID       string
			agentID            string
			runtimeID          pgtype.UUID
			ownerID            pgtype.UUID
			accessVersion      int64
			allowedReadSources []string
			writeSource        string
			connectionName     string
			toolsRaw           []byte
		)
		if err := rows.Scan(
			&permissionID,
			&agentID,
			&runtimeID,
			&ownerID,
			&accessVersion,
			&allowedReadSources,
			&writeSource,
			&connectionName,
			&toolsRaw,
		); err != nil {
			return nil, fmt.Errorf("scan current Company Brain target permission: %w", err)
		}
		target, err := l.loadTargetPermission(
			ctx,
			workspaceUUID,
			companyBrainConnectionID,
			permissionID,
			agentID,
			runtimeID,
			ownerID,
			accessVersion,
			allowedReadSources,
			writeSource,
			connectionName,
			toolsRaw,
			evidenceSources,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read current Company Brain target permissions: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AgentID == out[j].AgentID {
			return out[i].PermissionID < out[j].PermissionID
		}
		return out[i].AgentID < out[j].AgentID
	})
	return out, nil
}

func (l *DatabaseCurrentTargetPermissionLoader) loadTargetPermission(
	ctx context.Context,
	workspaceID pgtype.UUID,
	companyBrainConnectionID string,
	permissionID string,
	agentID string,
	runtimeID pgtype.UUID,
	ownerID pgtype.UUID,
	accessVersion int64,
	allowedReadSources []string,
	writeSource string,
	connectionName string,
	toolsRaw []byte,
	evidenceSources []string,
) (TargetPermission, error) {
	permissionUUID, err := util.ParseUUID(permissionID)
	if err != nil {
		return TargetPermission{}, fmt.Errorf("invalid target permission identity: %w", err)
	}
	agentUUID, err := util.ParseUUID(agentID)
	if err != nil {
		return TargetPermission{}, fmt.Errorf("invalid target permission agent identity: %w", err)
	}
	if accessVersion <= 0 {
		return TargetPermission{}, fmt.Errorf("target permission access version must be positive")
	}
	sources, ok := canonicalStrings(allowedReadSources, sourceID.MatchString)
	if !ok || !sourceID.MatchString(writeSource) || !containsString(sources, writeSource) {
		return TargetPermission{}, fmt.Errorf(
			"target permission %s has invalid source scope",
			util.UUIDToString(permissionUUID),
		)
	}
	if strings.TrimSpace(connectionName) == "" {
		return TargetPermission{}, fmt.Errorf("target permission connection name is required")
	}
	tools, err := parseCanonicalTargetTools(toolsRaw)
	if err != nil {
		return TargetPermission{}, fmt.Errorf(
			"target permission %s: %w",
			util.UUIDToString(permissionUUID),
			err,
		)
	}

	decisions := make([]ScopedToolDecision, 0, len(evidenceSources)*len(tools))
	for _, source := range evidenceSources {
		verdicts, err := l.policy.ConnectionToolVerdicts(ctx, toolpolicy.TableQuery{
			WorkspaceID: workspaceID,
			RuntimeID:   runtimeID,
			AgentID:     agentUUID,
			UserID:      ownerID,
			RequestContext: toolpolicy.RequestContext{
				ArgValues: map[string]string{"source_id": source},
			},
		})
		if err != nil {
			return TargetPermission{}, fmt.Errorf(
				"resolve target permission %s for source %s: %w",
				util.UUIDToString(permissionUUID),
				source,
				err,
			)
		}
		sourceDecisions, err := exactTargetToolDecisions(
			verdicts,
			connectionName,
			source,
			tools,
		)
		if err != nil {
			return TargetPermission{}, fmt.Errorf(
				"target permission %s: %w",
				util.UUIDToString(permissionUUID),
				err,
			)
		}
		decisions = append(decisions, sourceDecisions...)
	}
	sortDecisions(decisions)
	return TargetPermission{
		PermissionID:             util.UUIDToString(permissionUUID),
		CompanyBrainConnectionID: companyBrainConnectionID,
		AgentID:                  util.UUIDToString(agentUUID),
		AccessVersion:            accessVersion,
		AllowedReadSources:       sources,
		WriteSource:              writeSource,
		ApprovalOutcomes:         decisions,
		CanonicalToolCalls:       tools,
	}, nil
}

func targetEvidenceSources(report Report) ([]string, error) {
	sources := make([]string, 0, len(report.Connections))
	for _, connection := range report.Connections {
		source, ok := legacySourceName(connection.ConnectionName)
		if !ok {
			return nil, fmt.Errorf(
				"frozen census contains invalid Company Brain source %q",
				connection.ConnectionName,
			)
		}
		sources = append(sources, source)
	}
	sources, ok := canonicalStrings(sources, sourceID.MatchString)
	if !ok || len(sources) == 0 {
		return nil, fmt.Errorf("frozen census source evidence is empty or ambiguous")
	}
	return sources, nil
}

func parseCanonicalTargetTools(raw []byte) ([]string, error) {
	var discovered []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &discovered); err != nil {
		return nil, fmt.Errorf("decode canonical tool catalog: %w", err)
	}
	names := make([]string, 0, len(discovered))
	for _, tool := range discovered {
		names = append(names, tool.Name)
	}
	names, ok := canonicalStrings(names, validTool)
	if !ok || len(names) == 0 {
		return nil, fmt.Errorf("canonical tool catalog is empty or invalid")
	}
	return names, nil
}

func exactTargetToolDecisions(
	verdicts []toolpolicy.ConnectionToolVerdict,
	connectionName string,
	source string,
	canonicalTools []string,
) ([]ScopedToolDecision, error) {
	byTool := make(map[string]string, len(canonicalTools))
	for _, verdict := range verdicts {
		if verdict.Connection != connectionName {
			continue
		}
		if _, duplicate := byTool[verdict.Tool]; duplicate {
			return nil, fmt.Errorf(
				"duplicate target verdict for source %s and tool %s",
				source,
				verdict.Tool,
			)
		}
		decision := string(verdict.Setting)
		if !validDecision(decision) {
			return nil, fmt.Errorf(
				"target verdict for source %s and tool %s is not Allow/Ask/Deny",
				source,
				verdict.Tool,
			)
		}
		byTool[verdict.Tool] = decision
	}
	resolvedTools := make([]string, 0, len(byTool))
	for tool := range byTool {
		resolvedTools = append(resolvedTools, tool)
	}
	sort.Strings(resolvedTools)
	if !equalStrings(resolvedTools, canonicalTools) {
		return nil, fmt.Errorf(
			"target source %s lacks complete canonical tool evidence",
			source,
		)
	}
	out := make([]ScopedToolDecision, 0, len(canonicalTools))
	for _, tool := range canonicalTools {
		out = append(out, ScopedToolDecision{
			Source: source, Tool: tool, Decision: byTool[tool],
		})
	}
	return out, nil
}

const currentTargetPermissionsSQL = `
	SELECT
		target_permission.id::text,
		target_agent.id::text,
		target_agent.runtime_id,
		target_agent.owner_id,
		target_permission.company_brain_access_version,
		target_permission.company_brain_allowed_read_sources,
		target_permission.company_brain_write_source,
		target_connection.name,
		target_connection.tools
	FROM cerebro_company_brain_connection AS logical_connection
	JOIN workspace_connection AS target_connection
	  ON target_connection.workspace_id = logical_connection.workspace_id
	 AND target_connection.id = logical_connection.connection_id
	JOIN cerebro_tool_policy AS target_permission
	  ON target_permission.workspace_id = logical_connection.workspace_id
	 AND target_permission.company_brain_connection_id = logical_connection.id
	JOIN agent AS target_agent
	  ON target_agent.workspace_id = logical_connection.workspace_id
	 AND target_agent.id = target_permission.subject_id
	WHERE logical_connection.workspace_id = $1
	  AND logical_connection.id = $2
	  AND target_permission.layer = 'agent'
	  AND target_permission.setting = 'allow'
	  AND target_permission.resource_pattern = ''
	  AND target_permission.tool_key = 'connection:' || target_connection.name
	  AND target_permission.company_brain_lifecycle_state IN ('draft', 'active')
	ORDER BY target_agent.id, target_permission.id
`
