package runtimepool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
)

type Requirements struct {
	SchemaVersion   string   `json:"schema_version"`
	CapabilitiesAll []string `json:"capabilities_all"`
}

const (
	RequirementsSchemaV1          = "multica.runtime-requirements/v1"
	CapabilityAgentExecuteV1      = "multica.agent.execute/v1"
	CapabilityExtensionExecuteV1  = "multica.extension.execute/v1"
	BindingFixed                  = "fixed"
	BindingPool                   = "pool"
	SessionAffinityUnresolved     = "unresolved"
	SessionAffinityNone           = "none"
	SessionAffinityPinned         = "pinned"
	SessionAffinityRemoved        = "removed"
	StatusWaitingRuntime          = "waiting_runtime"
	MaxCapabilities               = 32
	MaxCapabilityBytes            = 128
	MaxCanonicalRequirementsBytes = 4096
)

var capabilityNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,127}$`)

func validateRequirements(value Requirements) error {
	if value.SchemaVersion != RequirementsSchemaV1 {
		return errors.New("unsupported requirements schema")
	}
	if len(value.CapabilitiesAll) == 0 || len(value.CapabilitiesAll) > MaxCapabilities {
		return errors.New("capabilities_all must contain 1..32 items")
	}
	if !sort.StringsAreSorted(value.CapabilitiesAll) {
		return errors.New("capabilities_all must be sorted")
	}
	for i, capability := range value.CapabilitiesAll {
		if len(capability) > MaxCapabilityBytes || !capabilityNameRE.MatchString(capability) {
			return errors.New("invalid capability")
		}
		if i > 0 && capability == value.CapabilitiesAll[i-1] {
			return errors.New("duplicate capability")
		}
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(canonical) > MaxCanonicalRequirementsBytes {
		return errors.New("canonical requirements exceed 4096 bytes")
	}
	return nil
}

func ParseRequirements(raw json.RawMessage) (Requirements, error) {
	if err := rejectDuplicateObjectKeys(raw); err != nil {
		return Requirements{}, err
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var value Requirements
	if err := decoder.Decode(&value); err != nil {
		return Requirements{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Requirements{}, errors.New("trailing JSON value")
	}
	if err := validateRequirements(value); err != nil {
		return Requirements{}, err
	}
	return value, nil
}

func rejectDuplicateObjectKeys(raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("trailing JSON value")
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if _, exists := keys[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			keys[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
		return nil
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
		return nil
	default:
		return errors.New("invalid JSON delimiter")
	}
}

func CanonicalRequirements(value Requirements) (json.RawMessage, error) {
	if err := validateRequirements(value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
