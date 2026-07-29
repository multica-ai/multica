// Package companybraincensus builds the read-only Company Brain migration census.
package companybraincensus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const claimTool = "whoami"

var sourceID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var legacyReferencePattern = regexp.MustCompile(`mcp__company-brain(?:-[a-z0-9-]+)?__[a-zA-Z0-9_-]+|company-brain-[a-z0-9-]+`)

type verificationStatus string
type censusErrorCode string

const (
	statusVerified     verificationStatus = "verified"
	statusUnverifiable verificationStatus = "unverifiable"

	errorUpstreamUnavailable           censusErrorCode = "upstream_unavailable"
	errorCallerAccessDenied            censusErrorCode = "caller_access_denied"
	errorInvalidIdentityClaim          censusErrorCode = "invalid_identity_claim"
	errorActorAccessUnavailable        censusErrorCode = "actor_access_unavailable"
	errorApprovalRequired              censusErrorCode = "approval_required"
	errorAccessDenied                  censusErrorCode = "access_denied"
	errorAutomationIdentityUnavailable censusErrorCode = "automation_identity_unavailable"
)

var errCallerAccessDenied = errors.New("caller cannot use Company Brain identity tool")

// Claim is the deliberately small, non-secret identity contract consumed by the census.
type Claim struct {
	WriteSource        string   `json:"write_source"`
	AllowedReadSources []string `json:"allowed_read_sources"`
}

type connectionClaim struct {
	ConnectionID   string             `json:"connection_id"`
	ConnectionName string             `json:"connection_name"`
	Claim          *Claim             `json:"claim,omitempty"`
	ToolAccess     []toolAccess       `json:"tool_access,omitempty"`
	Status         verificationStatus `json:"status"`
	ErrorCode      censusErrorCode    `json:"error_code,omitempty"`
}

type toolAccess struct {
	Tool     string `json:"tool"`
	Decision string `json:"decision"`
}

type actor struct {
	AgentID string            `json:"agent_id"`
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	Sources []connectionClaim `json:"sources"`
}

type automation struct {
	AutomationID string            `json:"automation_id"`
	Title        string            `json:"title"`
	Status       string            `json:"status"`
	AssigneeType string            `json:"assignee_type"`
	AssigneeID   string            `json:"assignee_id,omitempty"`
	TriggerKinds []string          `json:"trigger_kinds"`
	Sources      []connectionClaim `json:"sources"`
}

type legacyReference struct {
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
	OwnerName string `json:"owner_name"`
	Field     string `json:"field"`
	Reference string `json:"reference"`
}

// Report contains only migration evidence. It intentionally has no connection auth,
// raw MCP response, token, client id, or error text field.
type Report struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Actors      []actor           `json:"actors"`
	Automations []automation      `json:"automations"`
	Connections []connectionClaim `json:"connections"`
	References  []legacyReference `json:"references"`
}

type caller func(context.Context, connections.Connection) (json.RawMessage, error)

// actorAccess is the policy outcome for the diagnostic whoami call. A claim is
// evidence for an actor only after that actor is allowed to make the same call.
type actorAccess uint8

const (
	accessAllowed actorAccess = iota
	accessApprovalRequired
	accessDenied
)

type policySnapshot struct {
	Whoami actorAccess
	Tools  []toolAccess
}

type accessResolver func(context.Context, db.Agent, connections.Connection) (policySnapshot, error)
type automationAccessResolver func(context.Context, db.Agent, db.Autopilot, connections.Connection) (policySnapshot, error)

func companyBrainConnections(conns []connections.Connection) []connections.Connection {
	out := make([]connections.Connection, 0, len(conns))
	for _, conn := range conns {
		if !conn.Enabled || conn.Type != connections.TypeMCPHTTP {
			continue
		}
		if conn.Name == "company-brain" || strings.HasPrefix(conn.Name, "company-brain-") {
			out = append(out, conn)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Build collects one source claim per legacy connection, then attaches it only
// to actors whose effective policy allows the diagnostic whoami call. A missing,
// malformed, failed, denied, or approval-gated claim stays unverifiable; it is
// never inferred or widened.
func Build(
	ctx context.Context,
	agents []db.Agent,
	autopilots []db.ListAutopilotsRow,
	conns []connections.Connection,
	call caller,
	access accessResolver,
	automationAccess automationAccessResolver,
	now time.Time,
) Report {
	claims := make([]connectionClaim, 0)
	for _, conn := range companyBrainConnections(conns) {
		row := connectionClaim{ConnectionID: conn.ID, ConnectionName: conn.Name, Status: statusUnverifiable}
		raw, err := call(ctx, conn)
		if errors.Is(err, errCallerAccessDenied) {
			row.ErrorCode = errorCallerAccessDenied
		} else if err != nil {
			row.ErrorCode = errorUpstreamUnavailable
		} else if claim, err := parseClaim(raw); err != nil {
			row.ErrorCode = errorInvalidIdentityClaim
		} else {
			row.Claim = &claim
			row.Status = statusVerified
		}
		claims = append(claims, row)
	}

	actors := make([]actor, 0, len(agents))
	agentsByID := make(map[pgtype.UUID]db.Agent, len(agents))
	for _, agent := range agents {
		agentsByID[agent.ID] = agent
		if strings.EqualFold(strings.TrimSpace(agent.Status), "offline") {
			continue
		}
		sources := claimsForActor(ctx, agent, claims, conns, access)
		actors = append(actors, actor{AgentID: agent.ID.String(), Name: agent.Name, Status: agent.Status, Sources: sources})
	}
	sort.Slice(actors, func(i, j int) bool { return actors[i].Name < actors[j].Name })

	automations := make([]automation, 0, len(autopilots))
	for _, row := range autopilots {
		item := row.Autopilot
		sources := unverifiableClaims(claims, errorAutomationIdentityUnavailable)
		if item.AssigneeType == "agent" {
			if agent, ok := agentsByID[item.AssigneeID]; ok && automationAccess != nil {
				sources = claimsForActor(ctx, agent, claims, conns, func(ctx context.Context, agent db.Agent, conn connections.Connection) (policySnapshot, error) {
					return automationAccess(ctx, agent, item, conn)
				})
			}
		}
		automations = append(automations, automation{
			AutomationID: item.ID.String(),
			Title:        item.Title,
			Status:       item.Status,
			AssigneeType: item.AssigneeType,
			AssigneeID:   item.AssigneeID.String(),
			TriggerKinds: append([]string(nil), row.TriggerKinds...),
			Sources:      sources,
		})
	}
	sort.Slice(automations, func(i, j int) bool { return automations[i].Title < automations[j].Title })
	return Report{
		GeneratedAt: now.UTC(),
		Actors:      actors,
		Automations: automations,
		Connections: claims,
		References:  scanLegacyReferences(agents, autopilots, conns),
	}
}

func scanLegacyReferences(agents []db.Agent, autopilots []db.ListAutopilotsRow, conns []connections.Connection) []legacyReference {
	out := make([]legacyReference, 0)
	knownReferences := make(map[string]struct{})
	for _, conn := range companyBrainConnections(conns) {
		knownReferences[conn.Name] = struct{}{}
		for _, tool := range conn.Tools {
			knownReferences["mcp__"+conn.Name+"__"+tool.Name] = struct{}{}
		}
	}
	isKnownReference := func(reference string) bool {
		_, ok := knownReferences[reference]
		return ok
	}
	add := func(ownerType, ownerID, ownerName, field, value string) {
		seen := map[string]struct{}{}
		for _, match := range legacyReferencePattern.FindAllString(value, -1) {
			if !isKnownReference(match) {
				continue
			}
			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}
			out = append(out, legacyReference{
				OwnerType: ownerType,
				OwnerID:   ownerID,
				OwnerName: ownerName,
				Field:     field,
				Reference: match,
			})
		}
	}
	for _, agent := range agents {
		id := agent.ID.String()
		add("agent", id, agent.Name, "instructions", agent.Instructions)
		add("agent", id, agent.Name, "mcp_config", string(agent.McpConfig))
		add("agent", id, agent.Name, "runtime_config", string(agent.RuntimeConfig))
		add("agent", id, agent.Name, "custom_args", string(agent.CustomArgs))
		add("agent", id, agent.Name, "custom_env", string(agent.CustomEnv))
	}
	for _, row := range autopilots {
		item := row.Autopilot
		id := item.ID.String()
		add("automation", id, item.Title, "description", item.Description.String)
		add("automation", id, item.Title, "issue_title_template", item.IssueTitleTemplate.String)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i].OwnerType + out[i].OwnerName + out[i].Field + out[i].Reference
		right := out[j].OwnerType + out[j].OwnerName + out[j].Field + out[j].Reference
		return left < right
	})
	return out
}

func unverifiableClaims(claims []connectionClaim, code censusErrorCode) []connectionClaim {
	out := make([]connectionClaim, 0, len(claims))
	for _, claim := range claims {
		claim.Claim = nil
		claim.Status = statusUnverifiable
		claim.ErrorCode = code
		out = append(out, claim)
	}
	return out
}

func claimsForActor(ctx context.Context, agent db.Agent, claims []connectionClaim, conns []connections.Connection, access accessResolver) []connectionClaim {
	connectionsByID := make(map[string]connections.Connection, len(conns))
	for _, conn := range conns {
		connectionsByID[conn.ID] = conn
	}

	sources := make([]connectionClaim, 0, len(claims))
	for _, claim := range claims {
		conn, ok := connectionsByID[claim.ConnectionID]
		if !ok || access == nil {
			if claim.Status == statusVerified && claim.Claim != nil {
				claim.Claim = nil
				claim.Status = statusUnverifiable
				claim.ErrorCode = errorActorAccessUnavailable
			}
			sources = append(sources, claim)
			continue
		}

		policy, err := access(ctx, agent, conn)
		claim.ToolAccess = append([]toolAccess(nil), policy.Tools...)
		// Policy evidence is useful even when the upstream identity check failed.
		// Preserve that upstream error and do not invent an identity claim.
		if claim.Status != statusVerified || claim.Claim == nil {
			sources = append(sources, claim)
			continue
		}
		if err != nil {
			claim.Claim = nil
			claim.Status = statusUnverifiable
			claim.ErrorCode = errorActorAccessUnavailable
		} else if policy.Whoami == accessApprovalRequired {
			claim.Claim = nil
			claim.Status = statusUnverifiable
			claim.ErrorCode = errorApprovalRequired
		} else if policy.Whoami != accessAllowed {
			claim.Claim = nil
			claim.Status = statusUnverifiable
			claim.ErrorCode = errorAccessDenied
		}
		sources = append(sources, claim)
	}
	return sources
}

func parseClaim(raw json.RawMessage) (Claim, error) {
	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.IsError {
		return Claim{}, fmt.Errorf("invalid tool result")
	}
	for _, content := range envelope.Content {
		if content.Type != "text" || content.Text == "" {
			continue
		}
		var identity struct {
			Transport          string    `json:"transport"`
			WriteSource        *string   `json:"write_source"`
			AllowedReadSources *[]string `json:"allowed_read_sources"`
		}
		if json.Unmarshal([]byte(content.Text), &identity) != nil || (identity.Transport != "oauth" && identity.Transport != "legacy") || identity.WriteSource == nil || identity.AllowedReadSources == nil {
			continue
		}
		write := strings.TrimSpace(*identity.WriteSource)
		if !sourceID.MatchString(write) {
			continue
		}
		reads := make([]string, 0, len(*identity.AllowedReadSources))
		seen := map[string]struct{}{}
		for _, value := range *identity.AllowedReadSources {
			value = strings.TrimSpace(value)
			if !sourceID.MatchString(value) {
				return Claim{}, fmt.Errorf("invalid read source")
			}
			if _, exists := seen[value]; !exists {
				seen[value] = struct{}{}
				reads = append(reads, value)
			}
		}
		if len(reads) == 0 {
			reads = append(reads, write)
		}
		sort.Strings(reads)
		return Claim{WriteSource: write, AllowedReadSources: reads}, nil
	}
	return Claim{}, fmt.Errorf("identity claims absent")
}
