// Package issueproperty owns the transport-independent validation contract for
// values stored in an issue's custom-property bag. HTTP PUT and atomic issue
// creation both call this package so the accepted types and canonical form
// cannot drift between write paths.
package issueproperty

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	maxTextValueLen = 2000
	maxURLValueLen  = 2048
	// MaxActorValues is exported so the handler's existing focused tests can
	// continue to pin the public multi-actor limit after validation moved here.
	MaxActorValues = 20
)

var actorKinds = []string{"member"}

type propertyOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type propertyConfig struct {
	Options []propertyOption `json:"options,omitempty"`
}

// ActorRef is the canonical parsed form of a "<kind>:<uuid>" property value.
type ActorRef struct {
	Kind string
	ID   string
}

func (a ActorRef) String() string { return a.Kind + ":" + a.ID }

// IsActor reports whether a definition needs workspace-member resolution in
// addition to pure JSON shape validation.
func IsActor(propertyType string) bool {
	return propertyType == "actor" || propertyType == "multi_actor"
}

func ActorKindsHint() string { return strings.Join(actorKinds, " / ") }

// ParseActorRef validates and canonicalizes an actor reference. Members use
// user_id, matching the assignee contract and client-side member directory.
func ParseActorRef(value string) (ActorRef, error) {
	kind, id, found := strings.Cut(value, ":")
	if !found {
		return ActorRef{}, fmt.Errorf("value must look like \"<kind>:<uuid>\" where kind is one of: %s", ActorKindsHint())
	}
	valid := false
	for _, candidate := range actorKinds {
		if kind == candidate {
			valid = true
			break
		}
	}
	if !valid {
		return ActorRef{}, fmt.Errorf("unknown actor kind %q; valid kinds: %s", kind, ActorKindsHint())
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return ActorRef{}, fmt.Errorf("actor id in %q must be a UUID", value)
	}
	return ActorRef{Kind: kind, ID: parsed.String()}, nil
}

// ParseActorRefList validates a multi_actor array, removes duplicates, and
// preserves first-occurrence order.
func ParseActorRefList(items []any) ([]ActorRef, error) {
	if len(items) == 0 {
		return nil, errors.New("value must be a non-empty array of actor references")
	}
	if len(items) > MaxActorValues {
		return nil, fmt.Errorf("value cannot list more than %d actors", MaxActorValues)
	}
	seen := make(map[string]struct{}, len(items))
	refs := make([]ActorRef, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok {
			return nil, errors.New("value must be an array of actor reference strings")
		}
		ref, err := ParseActorRef(value)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[ref.String()]; duplicate {
			continue
		}
		seen[ref.String()] = struct{}{}
		refs = append(refs, ref)
	}
	return refs, nil
}

// ActorRefsInValue decodes canonical actor JSON for tenant-scoped reference
// resolution by the caller.
func ActorRefsInValue(propertyType string, stored []byte) ([]ActorRef, error) {
	if propertyType == "actor" {
		var value string
		if err := json.Unmarshal(stored, &value); err != nil {
			return nil, err
		}
		ref, err := ParseActorRef(value)
		if err != nil {
			return nil, err
		}
		return []ActorRef{ref}, nil
	}
	var values []string
	if err := json.Unmarshal(stored, &values); err != nil {
		return nil, err
	}
	refs := make([]ActorRef, 0, len(values))
	for _, value := range values {
		ref, err := ParseActorRef(value)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func parseConfig(raw []byte) propertyConfig {
	var config propertyConfig
	if len(raw) != 0 {
		_ = json.Unmarshal(raw, &config)
	}
	return config
}

func optionOrder(config propertyConfig) map[string]int {
	order := make(map[string]int, len(config.Options))
	for index, option := range config.Options {
		order[option.ID] = index
	}
	return order
}

func optionsHint(config propertyConfig) string {
	parts := make([]string, len(config.Options))
	for index, option := range config.Options {
		parts[index] = fmt.Sprintf("%s (%s)", option.ID, option.Name)
	}
	return strings.Join(parts, ", ")
}

// ValidateValue checks a raw JSON value against the definition's type and
// returns the canonical JSON stored by every issue-property write path.
func ValidateValue(def db.IssueProperty, raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("value is required")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("value must be valid JSON: %w", err)
	}
	if value == nil {
		return nil, errors.New("value cannot be null (use DELETE to unset a property)")
	}

	config := parseConfig(def.Config)
	switch def.Type {
	case "text":
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("value must be a string")
		}
		if strings.TrimSpace(text) == "" {
			return nil, errors.New("value cannot be empty (use DELETE to unset a property)")
		}
		if utf8.RuneCountInString(text) > maxTextValueLen {
			return nil, fmt.Errorf("value must be %d characters or fewer", maxTextValueLen)
		}
		return json.Marshal(util.SanitizeTextForPostgres(text))
	case "url":
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("value must be a URL string")
		}
		text = strings.TrimSpace(text)
		if len(text) > maxURLValueLen {
			return nil, fmt.Errorf("value must be %d characters or fewer", maxURLValueLen)
		}
		parsed, err := url.Parse(text)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, errors.New("value must be an http(s) URL")
		}
		return json.Marshal(text)
	case "number":
		if _, ok := value.(float64); !ok {
			return nil, errors.New("value must be a number")
		}
		return json.Marshal(value)
	case "checkbox":
		if _, ok := value.(bool); !ok {
			return nil, errors.New("value must be true or false")
		}
		return json.Marshal(value)
	case "date":
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("value must be a date string in YYYY-MM-DD format")
		}
		if _, err := time.Parse("2006-01-02", text); err != nil {
			return nil, errors.New("value must be a date string in YYYY-MM-DD format")
		}
		return json.Marshal(text)
	case "select":
		id, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("value must be one of the option ids: %s", optionsHint(config))
		}
		if _, exists := optionOrder(config)[id]; !exists {
			return nil, fmt.Errorf("value must be one of the option ids: %s", optionsHint(config))
		}
		return json.Marshal(id)
	case "multi_select":
		items, ok := value.([]any)
		if !ok || len(items) == 0 {
			return nil, fmt.Errorf("value must be a non-empty array of option ids: %s", optionsHint(config))
		}
		order := optionOrder(config)
		seen := make(map[string]struct{}, len(items))
		ids := make([]string, 0, len(items))
		for _, item := range items {
			id, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("value must be a non-empty array of option ids: %s", optionsHint(config))
			}
			if _, exists := order[id]; !exists {
				return nil, fmt.Errorf("unknown option id %q; valid option ids: %s", id, optionsHint(config))
			}
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		sort.SliceStable(ids, func(left, right int) bool { return order[ids[left]] < order[ids[right]] })
		return json.Marshal(ids)
	case "actor":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("value must be an actor reference string like \"member:<uuid>\" (kinds: %s)", ActorKindsHint())
		}
		ref, err := ParseActorRef(text)
		if err != nil {
			return nil, err
		}
		return json.Marshal(ref.String())
	case "multi_actor":
		items, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("value must be an array of actor reference strings like \"member:<uuid>\" (kinds: %s)", ActorKindsHint())
		}
		refs, err := ParseActorRefList(items)
		if err != nil {
			return nil, err
		}
		out := make([]string, len(refs))
		for index, ref := range refs {
			out[index] = ref.String()
		}
		return json.Marshal(out)
	default:
		return nil, fmt.Errorf("unsupported property type %q", def.Type)
	}
}
