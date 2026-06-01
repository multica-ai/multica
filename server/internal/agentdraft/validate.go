package agentdraft

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/skillindex"
	"github.com/multica-ai/multica/server/internal/util"
)

const MaxSummaryRunes = 500

type Result struct {
	AgentID         string   `json:"agent_id"`
	Name            string   `json:"name"`
	Summary         string   `json:"summary"`
	SkillSourceURLs []string `json:"skill_source_urls,omitempty"`
}

func NormalizeAgentDraftResult(raw []byte) ([]byte, Result, error) {
	var result Result
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, Result{}, fmt.Errorf("agent draft result must be a JSON object: %w", err)
	}

	result.AgentID = strings.TrimSpace(result.AgentID)
	result.Name = strings.TrimSpace(result.Name)
	result.Summary = strings.TrimSpace(result.Summary)
	if result.AgentID == "" {
		return nil, Result{}, fmt.Errorf("agent draft result has empty agent_id")
	}
	if _, err := util.ParseUUID(result.AgentID); err != nil {
		return nil, Result{}, fmt.Errorf("agent draft result has invalid agent_id: %w", err)
	}
	if result.Name == "" {
		return nil, Result{}, fmt.Errorf("agent draft result has empty name")
	}
	if result.Summary == "" {
		return nil, Result{}, fmt.Errorf("agent draft result has empty summary")
	}
	if len([]rune(result.Summary)) > MaxSummaryRunes {
		return nil, Result{}, fmt.Errorf("agent draft summary is too long")
	}

	allowed, err := skillindex.SourceURLSet()
	if err != nil {
		return nil, Result{}, err
	}
	seen := map[string]struct{}{}
	urls := make([]string, 0, len(result.SkillSourceURLs))
	for i, sourceURL := range result.SkillSourceURLs {
		sourceURL = strings.TrimSpace(sourceURL)
		if sourceURL == "" {
			return nil, Result{}, fmt.Errorf("agent draft skill_source_urls[%d] is empty", i)
		}
		if _, ok := allowed[sourceURL]; !ok {
			return nil, Result{}, fmt.Errorf("agent draft skill_source_urls[%d] is outside curated index", i)
		}
		if _, ok := seen[sourceURL]; ok {
			continue
		}
		seen[sourceURL] = struct{}{}
		urls = append(urls, sourceURL)
	}
	result.SkillSourceURLs = urls

	normalized, err := json.Marshal(result)
	if err != nil {
		return nil, Result{}, fmt.Errorf("marshal normalized agent draft result: %w", err)
	}
	return normalized, result, nil
}
