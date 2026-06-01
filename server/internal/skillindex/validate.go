package skillindex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	MaxSkillFindRecommendations = 5
	MaxReasonRunes              = 320
)

type Recommendation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SourceURL   string `json:"source_url"`
	Reason      string `json:"reason"`
}

func NormalizeSkillFindResult(raw []byte) ([]byte, []Recommendation, error) {
	var recs []Recommendation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&recs); err != nil {
		return nil, nil, fmt.Errorf("skill-find result must be a JSON array: %w", err)
	}
	if len(recs) == 0 {
		return nil, nil, fmt.Errorf("skill-find result must include at least one recommendation")
	}
	if len(recs) > MaxSkillFindRecommendations {
		return nil, nil, fmt.Errorf("skill-find result has %d recommendations, max %d", len(recs), MaxSkillFindRecommendations)
	}
	allowed, err := SourceURLSet()
	if err != nil {
		return nil, nil, err
	}
	for i := range recs {
		recs[i].Name = strings.TrimSpace(recs[i].Name)
		recs[i].Description = strings.TrimSpace(recs[i].Description)
		recs[i].SourceURL = strings.TrimSpace(recs[i].SourceURL)
		recs[i].Reason = strings.TrimSpace(recs[i].Reason)
		if recs[i].Name == "" {
			return nil, nil, fmt.Errorf("recommendation %d has empty name", i+1)
		}
		if recs[i].SourceURL == "" {
			return nil, nil, fmt.Errorf("recommendation %d has empty source_url", i+1)
		}
		if _, ok := allowed[recs[i].SourceURL]; !ok {
			return nil, nil, fmt.Errorf("recommendation %d uses source_url outside curated index", i+1)
		}
		if len([]rune(recs[i].Reason)) > MaxReasonRunes {
			return nil, nil, fmt.Errorf("recommendation %d reason is too long", i+1)
		}
	}
	normalized, err := json.Marshal(recs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal normalized skill-find result: %w", err)
	}
	return normalized, recs, nil
}
