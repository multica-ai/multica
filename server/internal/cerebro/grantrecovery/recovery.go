package grantrecovery

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	ApprovalCapability = "permission_recovery:apply"
	ApprovalBoundary   = "production_permission_recovery_import"
	ApprovalIssueID    = "72b52abc-6d80-4611-8fd8-5a164602d788"
)

func ApprovalResource(workspaceID, sourceFingerprint string) string {
	return "workspace:" + workspaceID + ":source:" + sourceFingerprint
}

type LegacyGrant struct {
	WorkspaceID string          `json:"workspace_id"`
	AgentID     string          `json:"agent_id"`
	ToolName    string          `json:"tool_name"`
	Enabled     bool            `json:"enabled"`
	Config      json.RawMessage `json:"config_json,omitempty"`
}

type Rule struct {
	WorkspaceID     string          `json:"workspace_id"`
	AgentID         string          `json:"agent_id"`
	ToolKey         string          `json:"tool_key"`
	Setting         string          `json:"setting"`
	ResourcePattern string          `json:"resource_pattern"`
	Conditions      json.RawMessage `json:"conditions,omitempty"`
}

type Unmapped struct {
	Grant  LegacyGrant `json:"grant"`
	Reason string      `json:"reason"`
}

type Conflict struct {
	Wanted   Rule `json:"wanted"`
	Existing Rule `json:"existing"`
}

type Diff struct {
	SourceFingerprint string     `json:"source_fingerprint"`
	Mapped            []Rule     `json:"mapped"`
	AlreadyPresent    []Rule     `json:"already_present"`
	Conflicting       []Conflict `json:"conflicting"`
	Unmapped          []Unmapped `json:"unmapped"`
}

func Transform(grants []LegacyGrant) ([]Rule, []Unmapped) {
	rules := make([]Rule, 0, len(grants))
	unmapped := make([]Unmapped, 0)
	for _, grant := range grants {
		grant.ToolName = strings.TrimSpace(grant.ToolName)
		if grant.WorkspaceID == "" || grant.AgentID == "" || grant.ToolName == "" {
			unmapped = append(unmapped, Unmapped{Grant: grant, Reason: "workspace, agent, and tool identity are required"})
			continue
		}
		base, extra, problem := transformGrant(grant)
		rules = append(rules, base...)
		if problem != "" {
			unmapped = append(unmapped, Unmapped{Grant: grant, Reason: problem})
		}
		rules = append(rules, extra...)
	}
	sortRules(rules)
	sort.Slice(unmapped, func(i, j int) bool { return grantIdentity(unmapped[i].Grant) < grantIdentity(unmapped[j].Grant) })
	return rules, unmapped
}

func transformGrant(grant LegacyGrant) ([]Rule, []Rule, string) {
	setting := "deny"
	if grant.Enabled {
		setting = "allow"
	}
	base := Rule{WorkspaceID: grant.WorkspaceID, AgentID: grant.AgentID, ToolKey: grant.ToolName, Setting: setting}
	if grant.ToolName != "firtal_registry" {
		if hasConfig(grant.Config) {
			return []Rule{base}, nil, "tool-specific legacy config has no canonical policy mapping"
		}
		return []Rule{base}, nil, ""
	}

	config := map[string]any{}
	if hasConfig(grant.Config) {
		if err := json.Unmarshal(grant.Config, &config); err != nil {
			return nil, nil, "invalid Registry config JSON"
		}
	}
	if grant.Enabled && hasConfig(grant.Config) {
		all, _ := config["allowed_data_sources_all"].(bool)
		values := stringSlice(config["allowed_data_sources"])
		switch {
		case all:
			base.Setting = "allow"
		case len(values) > 0:
			base.Setting = "allow"
			base.Conditions = mustJSON(map[string]any{"arg_allowlist": []any{map[string]any{"arg": "data_source_id", "values": values}}})
		default:
			base.Setting = "deny"
		}
	}

	extra := make([]Rule, 0, 2)
	if grant.Enabled {
		extra = append(extra,
			registryActionRule(grant, "action:list_apps", boolValue(config["allowed_apps"])),
			registryActionRule(grant, "action:update_app", boolValue(config["allow_write"])),
		)
	}
	return []Rule{base}, extra, ""
}

func registryActionRule(grant LegacyGrant, resource string, allowed bool) Rule {
	setting := "deny"
	if allowed {
		setting = "allow"
	}
	return Rule{WorkspaceID: grant.WorkspaceID, AgentID: grant.AgentID, ToolKey: "firtal_registry", Setting: setting, ResourcePattern: resource}
}

func BuildDiff(grants []LegacyGrant, existing []Rule, targetAgents map[string]bool) Diff {
	rules, unmapped := Transform(grants)
	diff := Diff{SourceFingerprint: Fingerprint(grants), Unmapped: unmapped}
	byIdentity := make(map[string]Rule, len(existing))
	for _, rule := range existing {
		byIdentity[ruleIdentity(rule)] = normalizedRule(rule)
	}
	for _, rule := range rules {
		if !targetAgents[rule.AgentID] {
			diff.Unmapped = append(diff.Unmapped, Unmapped{Grant: LegacyGrant{
				WorkspaceID: rule.WorkspaceID, AgentID: rule.AgentID, ToolName: rule.ToolKey,
			}, Reason: "agent does not exist in the target database"})
			continue
		}
		if current, ok := byIdentity[ruleIdentity(rule)]; ok {
			if equalRule(rule, current) {
				diff.AlreadyPresent = append(diff.AlreadyPresent, normalizedRule(rule))
			} else {
				diff.Conflicting = append(diff.Conflicting, Conflict{Wanted: normalizedRule(rule), Existing: current})
			}
			continue
		}
		diff.Mapped = append(diff.Mapped, normalizedRule(rule))
	}
	sortRules(diff.Mapped)
	sortRules(diff.AlreadyPresent)
	sort.Slice(diff.Conflicting, func(i, j int) bool {
		return ruleIdentity(diff.Conflicting[i].Wanted) < ruleIdentity(diff.Conflicting[j].Wanted)
	})
	sort.Slice(diff.Unmapped, func(i, j int) bool {
		return grantIdentity(diff.Unmapped[i].Grant) < grantIdentity(diff.Unmapped[j].Grant)
	})
	return diff
}

func Fingerprint(grants []LegacyGrant) string {
	copyOf := append([]LegacyGrant(nil), grants...)
	sort.Slice(copyOf, func(i, j int) bool { return grantIdentity(copyOf[i]) < grantIdentity(copyOf[j]) })
	raw, _ := json.Marshal(copyOf)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (d Diff) SafeToApply() bool { return len(d.Conflicting) == 0 && len(d.Unmapped) == 0 }

func ruleIdentity(rule Rule) string {
	return strings.Join([]string{rule.WorkspaceID, rule.AgentID, rule.ToolKey, rule.ResourcePattern}, "\x00")
}

func grantIdentity(grant LegacyGrant) string {
	return strings.Join([]string{grant.WorkspaceID, grant.AgentID, grant.ToolName}, "\x00")
}

func equalRule(left, right Rule) bool {
	left, right = normalizedRule(left), normalizedRule(right)
	return left.Setting == right.Setting && bytes.Equal(left.Conditions, right.Conditions)
}

func normalizedRule(rule Rule) Rule {
	rule.ToolKey = strings.TrimSpace(rule.ToolKey)
	rule.ResourcePattern = strings.TrimSpace(rule.ResourcePattern)
	rule.Conditions = canonicalJSON(rule.Conditions)
	return rule
}

func canonicalJSON(raw json.RawMessage) json.RawMessage {
	if !hasConfig(raw) || string(raw) == "null" {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return append(json.RawMessage(nil), raw...)
	}
	return mustJSON(value)
}

func hasConfig(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte("{}"))
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("marshal recovery JSON: %v", err))
	}
	return raw
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	sort.Strings(out)
	return out
}

func sortRules(rules []Rule) {
	sort.Slice(rules, func(i, j int) bool { return ruleIdentity(rules[i]) < ruleIdentity(rules[j]) })
}
