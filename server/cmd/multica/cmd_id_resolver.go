package main

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
)

const minShortIDPrefixLen = 4

type resolvedID struct {
	ID      string
	Display string
}

type idCandidate struct {
	ID      string
	Display string
	Detail  string
}

func displayID(id string, full bool) string {
	if full {
		return id
	}
	return truncateID(id)
}

func issueDisplayKey(issue map[string]any) string {
	if key := strVal(issue, "identifier"); key != "" {
		return key
	}
	return strVal(issue, "id")
}

func issueCandidate(issue map[string]any) idCandidate {
	return idCandidate{
		ID:      strVal(issue, "id"),
		Display: issueDisplayKey(issue),
		Detail:  strVal(issue, "title"),
	}
}

func normalizeUUIDPrefix(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", fmt.Errorf("id is required")
	}
	prefix := strings.ToLower(strings.ReplaceAll(trimmed, "-", ""))
	if len(prefix) < minShortIDPrefixLen {
		return "", fmt.Errorf("expected a full UUID or at least %d hex characters, got %q", minShortIDPrefixLen, input)
	}
	for _, r := range prefix {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return "", fmt.Errorf("expected a UUID prefix containing only hex characters, got %q", input)
		}
	}
	return prefix, nil
}

func compactUUID(id string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(id), "-", ""))
}

func resolveIDByPrefix(ctx context.Context, client *cli.APIClient, kind, input string, fetch func(context.Context, *cli.APIClient) ([]idCandidate, error)) (resolvedID, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return resolvedID{}, fmt.Errorf("%s id is required", kind)
	}
	if uuidRegexp.MatchString(trimmed) {
		return resolvedID{ID: trimmed, Display: trimmed}, nil
	}

	prefix, err := normalizeUUIDPrefix(trimmed)
	if err != nil {
		return resolvedID{}, fmt.Errorf("resolve %s: %w", kind, err)
	}

	candidates, err := fetch(ctx, client)
	if err != nil {
		return resolvedID{}, fmt.Errorf("resolve %s: %w", kind, err)
	}

	matches := make([]idCandidate, 0, 1)
	for _, c := range candidates {
		if c.ID == "" {
			continue
		}
		if strings.HasPrefix(compactUUID(c.ID), prefix) {
			matches = append(matches, c)
		}
	}

	switch len(matches) {
	case 0:
		return resolvedID{}, fmt.Errorf("no %s found matching id prefix %q; run the list command with --full-id to copy the full UUID", kind, input)
	case 1:
		display := matches[0].Display
		if display == "" {
			display = matches[0].ID
		}
		return resolvedID{ID: matches[0].ID, Display: display}, nil
	default:
		return resolvedID{}, ambiguousIDPrefixError(kind, input, matches)
	}
}

func ambiguousIDPrefixError(kind, input string, matches []idCandidate) error {
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Display == matches[j].Display {
			return matches[i].ID < matches[j].ID
		}
		return matches[i].Display < matches[j].Display
	})
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		label := m.Display
		if label == "" {
			label = m.ID
		}
		if m.Detail != "" {
			label += " — " + m.Detail
		}
		parts = append(parts, fmt.Sprintf("  %s (%s)", label, m.ID))
	}
	return fmt.Errorf("ambiguous %s id prefix %q; matches:\n%s\nUse more characters or run the list command with --full-id", kind, input, strings.Join(parts, "\n"))
}

func resolveIssueRef(ctx context.Context, client *cli.APIClient, input string) (resolvedID, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return resolvedID{}, fmt.Errorf("issue id is required")
	}

	// Preserve issue-key semantics before considering UUID prefixes. This
	// mirrors the server-side loadIssueForUser order and avoids treating
	// strings like MUL-1852 as a UUID prefix.
	if looksLikeIssueIdentifier(trimmed) {
		return fetchIssueRef(ctx, client, trimmed)
	}
	if n, ok := parsePositiveInt(trimmed); ok {
		resolved, err := resolveIssueNumber(ctx, client, n)
		if err == nil {
			return resolved, nil
		}
		if _, prefixErr := normalizeUUIDPrefix(trimmed); prefixErr != nil {
			return resolvedID{}, err
		}
	}
	if uuidRegexp.MatchString(trimmed) {
		return fetchIssueRef(ctx, client, trimmed)
	}
	return resolveIDByPrefix(ctx, client, "issue", trimmed, fetchIssueCandidates)
}

func fetchIssueRef(ctx context.Context, client *cli.APIClient, ref string) (resolvedID, error) {
	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(ref), &issue); err != nil {
		return resolvedID{}, err
	}
	c := issueCandidate(issue)
	if c.Display == "" {
		c.Display = c.ID
	}
	return resolvedID{ID: c.ID, Display: c.Display}, nil
}

func looksLikeIssueIdentifier(input string) bool {
	if input == "" {
		return false
	}
	dash := strings.LastIndex(input, "-")
	if dash <= 0 || dash >= len(input)-1 {
		return false
	}
	prefix := input[:dash]
	for _, r := range prefix {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	_, ok := parsePositiveInt(input[dash+1:])
	return ok
}

func parsePositiveInt(input string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func resolveIssueNumber(ctx context.Context, client *cli.APIClient, number int) (resolvedID, error) {
	candidates, err := fetchIssueCandidates(ctx, client)
	if err != nil {
		return resolvedID{}, fmt.Errorf("resolve issue number: %w", err)
	}
	want := strconv.Itoa(number)
	for _, c := range candidates {
		if strings.HasSuffix(c.Display, "-"+want) {
			return resolvedID{ID: c.ID, Display: c.Display}, nil
		}
	}
	return resolvedID{}, fmt.Errorf("no issue found with number %d", number)
}

func fetchIssueCandidates(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required to resolve issue id prefixes")
	}
	const limit = 100
	candidates := []idCandidate{}
	for offset := 0; ; offset += limit {
		params := url.Values{}
		params.Set("workspace_id", client.WorkspaceID)
		params.Set("limit", strconv.Itoa(limit))
		if offset > 0 {
			params.Set("offset", strconv.Itoa(offset))
		}
		var result map[string]any
		if err := client.GetJSON(ctx, "/api/issues?"+params.Encode(), &result); err != nil {
			return nil, err
		}
		issuesRaw, _ := result["issues"].([]any)
		for _, raw := range issuesRaw {
			issue, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			candidates = append(candidates, issueCandidate(issue))
		}
		total, _ := result["total"].(float64)
		if len(issuesRaw) == 0 || offset+len(issuesRaw) >= int(total) {
			break
		}
	}
	return candidates, nil
}

func resolveAutopilotID(ctx context.Context, client *cli.APIClient, input string) (resolvedID, error) {
	return resolveIDByPrefix(ctx, client, "autopilot", input, fetchAutopilotCandidates)
}

func fetchAutopilotCandidates(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required to resolve autopilot id prefixes")
	}
	var resp struct {
		Autopilots []map[string]any `json:"autopilots"`
	}
	if err := client.GetJSON(ctx, "/api/autopilots", &resp); err != nil {
		return nil, err
	}
	candidates := make([]idCandidate, 0, len(resp.Autopilots))
	for _, a := range resp.Autopilots {
		candidates = append(candidates, idCandidate{
			ID:      strVal(a, "id"),
			Display: strVal(a, "title"),
			Detail:  strVal(a, "status"),
		})
	}
	return candidates, nil
}

func resolveAutopilotTriggerID(ctx context.Context, client *cli.APIClient, autopilotID, input string) (resolvedID, error) {
	trimmed := strings.TrimSpace(input)
	if uuidRegexp.MatchString(trimmed) {
		return resolvedID{ID: trimmed, Display: trimmed}, nil
	}
	fetch := func(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
		var resp map[string]any
		if err := client.GetJSON(ctx, "/api/autopilots/"+url.PathEscape(autopilotID), &resp); err != nil {
			return nil, err
		}
		triggersRaw, _ := resp["triggers"].([]any)
		candidates := make([]idCandidate, 0, len(triggersRaw))
		for _, raw := range triggersRaw {
			t, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			detail := strVal(t, "kind")
			if label := strVal(t, "label"); label != "" {
				detail = label
			}
			candidates = append(candidates, idCandidate{
				ID:      strVal(t, "id"),
				Display: strVal(t, "id"),
				Detail:  detail,
			})
		}
		return candidates, nil
	}
	return resolveIDByPrefix(ctx, client, "autopilot trigger", input, fetch)
}

func resolveProjectID(ctx context.Context, client *cli.APIClient, input string) (resolvedID, error) {
	return resolveIDByPrefix(ctx, client, "project", input, fetchProjectCandidates)
}

func fetchProjectCandidates(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required to resolve project id prefixes")
	}
	params := url.Values{"workspace_id": {client.WorkspaceID}}
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/projects?"+params.Encode(), &result); err != nil {
		return nil, err
	}
	projectsRaw, _ := result["projects"].([]any)
	candidates := make([]idCandidate, 0, len(projectsRaw))
	for _, raw := range projectsRaw {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		candidates = append(candidates, idCandidate{
			ID:      strVal(p, "id"),
			Display: strVal(p, "title"),
			Detail:  strVal(p, "status"),
		})
	}
	return candidates, nil
}

func resolveProjectResourceID(ctx context.Context, client *cli.APIClient, projectID, input string) (resolvedID, error) {
	fetch := func(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
		var result map[string]any
		if err := client.GetJSON(ctx, "/api/projects/"+url.PathEscape(projectID)+"/resources", &result); err != nil {
			return nil, err
		}
		resourcesRaw, _ := result["resources"].([]any)
		candidates := make([]idCandidate, 0, len(resourcesRaw))
		for _, raw := range resourcesRaw {
			r, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			display := strVal(r, "label")
			if display == "" {
				display = strVal(r, "resource_type")
			}
			candidates = append(candidates, idCandidate{
				ID:      strVal(r, "id"),
				Display: display,
				Detail:  summarizeResourceRef(r["resource_ref"]),
			})
		}
		return candidates, nil
	}
	return resolveIDByPrefix(ctx, client, "project resource", input, fetch)
}

func resolveLabelID(ctx context.Context, client *cli.APIClient, input string) (resolvedID, error) {
	return resolveIDByPrefix(ctx, client, "label", input, fetchLabelCandidates)
}

func fetchLabelCandidates(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required to resolve label id prefixes")
	}
	params := url.Values{"workspace_id": {client.WorkspaceID}}
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/labels?"+params.Encode(), &result); err != nil {
		return nil, err
	}
	labelsRaw, _ := result["labels"].([]any)
	candidates := make([]idCandidate, 0, len(labelsRaw))
	for _, raw := range labelsRaw {
		l, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		candidates = append(candidates, idCandidate{
			ID:      strVal(l, "id"),
			Display: strVal(l, "name"),
			Detail:  strVal(l, "color"),
		})
	}
	return candidates, nil
}

type actorDisplayLookup struct {
	members map[string]string
	agents  map[string]string
}

func loadActorDisplayLookup(ctx context.Context, client *cli.APIClient) actorDisplayLookup {
	lookup := actorDisplayLookup{
		members: map[string]string{},
		agents:  map[string]string{},
	}
	if client.WorkspaceID == "" {
		return lookup
	}
	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+client.WorkspaceID+"/members", &members); err == nil {
		for _, m := range members {
			if id := strVal(m, "user_id"); id != "" {
				lookup.members[id] = strVal(m, "name")
			}
		}
	}
	var agents []map[string]any
	agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
	if err := client.GetJSON(ctx, agentPath, &agents); err == nil {
		for _, a := range agents {
			if id := strVal(a, "id"); id != "" {
				lookup.agents[id] = strVal(a, "name")
			}
		}
	}
	return lookup
}

func (l actorDisplayLookup) actor(actorType, id string) string {
	if actorType == "" || id == "" {
		return ""
	}
	switch actorType {
	case "member":
		if name := l.members[id]; name != "" {
			return "member:" + name
		}
	case "agent":
		if name := l.agents[id]; name != "" {
			return "agent:" + name
		}
	}
	return actorType + ":" + id
}

func (l actorDisplayLookup) agent(id string) string {
	if id == "" {
		return ""
	}
	if name := l.agents[id]; name != "" {
		return name
	}
	return id
}
