package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

type devinModelChoice struct {
	Value   string             `json:"value"`
	Name    string             `json:"name"`
	Options []devinModelChoice `json:"options"`
}

type devinModelOption struct {
	ID           string             `json:"id"`
	Category     string             `json:"category"`
	CurrentValue string             `json:"currentValue"`
	Options      []devinModelChoice `json:"options"`
}

func devinSessionModel(raw json.RawMessage) devinModelOption {
	var response struct {
		ConfigOptions []json.RawMessage `json:"configOptions"`
	}
	if json.Unmarshal(raw, &response) == nil {
		for _, rawOption := range response.ConfigOptions {
			var option devinModelOption
			if json.Unmarshal(rawOption, &option) == nil && (option.Category == "model" || option.ID == "model") {
				return option
			}
		}
	}
	// Older CLI builds expose the same selected-model fact in models.
	return devinModelOption{CurrentValue: extractACPCurrentModelID(raw)}
}

func devinModelAvailable(choices []devinModelChoice, value string) bool {
	for _, choice := range choices {
		if choice.Value == value || devinModelAvailable(choice.Options, value) {
			return true
		}
	}
	return false
}

func normalizeDevinModelName(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}

func devinNamedModelValues(choices []devinModelChoice, name string, values map[string]bool) {
	for _, choice := range choices {
		if choice.Value != "" && normalizeDevinModelName(choice.Name) == name {
			values[choice.Value] = true
		}
		devinNamedModelValues(choice.Options, name, values)
	}
}

// Catalog aliases identify a family, never account entitlement. Do not infer
// versions from opaque model IDs or guess a winner across ambiguous families.
func devinCatalogModelValues(data []byte, requested string) (map[string]bool, error) {
	var catalog struct {
		Families []struct {
			Slug     string   `json:"slug"`
			Name     string   `json:"family_label"`
			Aliases  []string `json:"aliases"`
			Variants []struct {
				ID    string `json:"model_uid"`
				Label string `json:"label"`
			} `json:"variants"`
		} `json:"families"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("read Devin model aliases: %w", err)
	}
	name := normalizeDevinModelName(requested)
	values := make(map[string]bool)
	if name == "" {
		return values, nil
	}
	matchedFamilies := 0
	for _, family := range catalog.Families {
		familyMatch := false
		for _, alias := range append([]string{family.Slug, family.Name}, family.Aliases...) {
			if normalizeDevinModelName(alias) == name {
				familyMatch = true
				break
			}
		}
		matched := false
		for _, variant := range family.Variants {
			if variant.ID != "" && (familyMatch || variant.ID == requested || normalizeDevinModelName(variant.Label) == name) {
				values[variant.ID] = true
				matched = true
			}
		}
		if matched {
			matchedFamilies++
		}
	}
	if matchedFamilies > 1 {
		return nil, fmt.Errorf("Devin model name %q matches multiple families; choose a canonical model ID", requested)
	}
	return values, nil
}

// confirmDevinModel treats the session's state, not the launch flag or global
// catalog, as authoritative. The CLI can accept --model and silently use its
// configured default when that model is unavailable to the account. Session
// configOptions also let a resumed conversation apply a changed model choice.
func confirmDevinModel(ctx context.Context, request acpRequestFn, sessionID, requested string, raw json.RawMessage, catalog func(context.Context) ([]byte, error)) (string, error) {
	option := devinSessionModel(raw)
	requested = strings.TrimSpace(requested)
	if requested == "" || requested == option.CurrentValue {
		return option.CurrentValue, nil
	}
	values := map[string]bool{requested: true}
	if !devinModelAvailable(option.Options, requested) {
		values = make(map[string]bool)
		if name := normalizeDevinModelName(requested); name != "" {
			devinNamedModelValues(option.Options, name, values)
		}
		if len(values) > 1 {
			return "", fmt.Errorf("Devin model name %q matches multiple session choices; choose a canonical model ID", requested)
		}
		if len(values) == 0 && catalog != nil {
			data, err := catalog(ctx)
			if err != nil {
				return "", fmt.Errorf("resolve Devin model %q: %w; choose a canonical model ID", requested, err)
			}
			values, err = devinCatalogModelValues(data, requested)
			if err != nil {
				return "", err
			}
		}
	}
	if option.CurrentValue != "" && values[option.CurrentValue] {
		return option.CurrentValue, nil
	}
	// A fresh CLI may already have resolved an alias above. On resume, set a
	// canonical advertised value only if there is one unambiguous candidate.
	selected := ""
	for value := range values {
		if devinModelAvailable(option.Options, value) {
			if selected != "" {
				return "", fmt.Errorf("Devin model name %q has multiple available variants; choose a canonical model ID", requested)
			}
			selected = value
		}
	}
	if option.ID == "" || selected == "" {
		return "", fmt.Errorf("requested Devin model %q is unavailable or cannot be confirmed (session model %q); choose an available model ID or use the CLI default", requested, option.CurrentValue)
	}
	result, err := request(ctx, "session/set_config_option", map[string]any{
		"sessionId": sessionID,
		"configId":  option.ID,
		"value":     selected,
	})
	if err != nil {
		return "", fmt.Errorf("set Devin model %q: %w", requested, err)
	}
	actual := devinSessionModel(result).CurrentValue
	if actual != selected {
		return "", fmt.Errorf("Devin did not confirm requested model %q (session model %q); no prompt was sent", requested, actual)
	}
	return actual, nil
}
