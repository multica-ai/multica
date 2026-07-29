// Package companybraincensus builds the read-only Company Brain migration census.
package companybraincensus

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const claimTool = "whoami"

var sourceID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Claim is the deliberately small, non-secret identity contract consumed by the census.
type Claim struct {
	WriteSource        string   `json:"write_source"`
	AllowedReadSources []string `json:"allowed_read_sources"`
}

type connectionClaim struct {
	ConnectionID   string `json:"connection_id"`
	ConnectionName string `json:"connection_name"`
	Claim          *Claim `json:"claim,omitempty"`
	Status         string `json:"status"`
	ErrorCode      string `json:"error_code,omitempty"`
}

type actor struct {
	AgentID string            `json:"agent_id"`
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	Sources []connectionClaim `json:"sources"`
}

// Report contains only migration evidence. It intentionally has no connection auth,
// raw MCP response, token, client id, or error text field.
type Report struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Actors      []actor           `json:"actors"`
	Connections []connectionClaim `json:"connections"`
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

type accessResolver func(context.Context, db.Agent, connections.Connection) (actorAccess, error)

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
func Build(ctx context.Context, agents []db.Agent, conns []connections.Connection, call caller, access accessResolver, now time.Time) Report {
	claims := make([]connectionClaim, 0)
	for _, conn := range companyBrainConnections(conns) {
		row := connectionClaim{ConnectionID: conn.ID, ConnectionName: conn.Name, Status: "unverifiable"}
		raw, err := call(ctx, conn)
		if err != nil {
			row.ErrorCode = "upstream_unavailable"
		} else if claim, err := parseClaim(raw); err != nil {
			row.ErrorCode = "invalid_identity_claim"
		} else {
			row.Claim = &claim
			row.Status = "verified"
		}
		claims = append(claims, row)
	}

	actors := make([]actor, 0, len(agents))
	for _, agent := range agents {
		if strings.EqualFold(strings.TrimSpace(agent.Status), "offline") {
			continue
		}
		sources := claimsForActor(ctx, agent, claims, conns, access)
		actors = append(actors, actor{AgentID: agent.ID.String(), Name: agent.Name, Status: agent.Status, Sources: sources})
	}
	sort.Slice(actors, func(i, j int) bool { return actors[i].Name < actors[j].Name })
	return Report{GeneratedAt: now.UTC(), Actors: actors, Connections: claims}
}

func claimsForActor(ctx context.Context, agent db.Agent, claims []connectionClaim, conns []connections.Connection, access accessResolver) []connectionClaim {
	connectionsByID := make(map[string]connections.Connection, len(conns))
	for _, conn := range conns {
		connectionsByID[conn.ID] = conn
	}

	sources := make([]connectionClaim, 0, len(claims))
	for _, claim := range claims {
		// Preserve upstream identity failures. There is no claim to attach to any actor.
		if claim.Status != "verified" || claim.Claim == nil {
			sources = append(sources, claim)
			continue
		}

		conn, ok := connectionsByID[claim.ConnectionID]
		if !ok || access == nil {
			claim.Claim = nil
			claim.Status = "unverifiable"
			claim.ErrorCode = "actor_access_unavailable"
			sources = append(sources, claim)
			continue
		}

		decision, err := access(ctx, agent, conn)
		if err != nil {
			claim.Claim = nil
			claim.Status = "unverifiable"
			claim.ErrorCode = "actor_access_unavailable"
		} else if decision == accessApprovalRequired {
			claim.Claim = nil
			claim.Status = "unverifiable"
			claim.ErrorCode = "approval_required"
		} else if decision != accessAllowed {
			claim.Claim = nil
			claim.Status = "unverifiable"
			claim.ErrorCode = "access_denied"
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
		sort.Strings(reads)
		return Claim{WriteSource: write, AllowedReadSources: reads}, nil
	}
	return Claim{}, fmt.Errorf("identity claims absent")
}
