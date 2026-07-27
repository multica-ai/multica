package agent

import (
	"encoding/json"
	"strings"
)

const mutationPolicyEnv = "MULTICA_AGENT_MUTATION_POLICY"

// mutationPolicyDenies reports whether this runtime should fail closed on
// mutating agent actions. The phase-0 POC uses this to keep Multica's headless
// auto-approval paths available for read-only work while denying writes before
// they reach the provider tool runtime.
func mutationPolicyDenies(env map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(env[mutationPolicyEnv]), "deny")
}

func acpPermissionRequestMutates(params json.RawMessage) bool {
	if len(params) == 0 || string(params) == "null" {
		return true
	}
	var p struct {
		ToolCall struct {
			Title   string          `json:"title"`
			Name    string          `json:"name"`
			Kind    string          `json:"kind"`
			Content json.RawMessage `json:"content"`
		} `json:"toolCall"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return true
	}
	probe := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		p.ToolCall.Title,
		p.ToolCall.Name,
		p.ToolCall.Kind,
	}, " ")))
	if probe == "" {
		return true
	}
	// Permit-by-exception: only known read-only operation labels pass. Every
	// other ACP permission request is treated as mutating under the deny policy.
	for _, token := range []string{"read", "list", "search", "grep", "inspect", "view", "stat"} {
		if hasACPReadOnlyLabel(probe, token) {
			return false
		}
	}
	return true
}

func hasACPReadOnlyLabel(probe, label string) bool {
	probe = strings.TrimSpace(probe)
	return probe == label ||
		strings.HasPrefix(probe, label+":") ||
		strings.HasPrefix(probe, label+" ") ||
		strings.Contains(probe, " "+label+":") ||
		strings.Contains(probe, " "+label+" ")
}

func selectACPRejectOnce(params json.RawMessage) (optionID string, ok bool) {
	var p struct {
		Options []acpPermissionOption `json:"options"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return "", false
		}
	}
	for _, opt := range p.Options {
		if opt.OptionID != "" && strings.EqualFold(strings.TrimSpace(opt.Kind), acpKindRejectOnce) {
			return opt.OptionID, true
		}
	}
	return "", false
}

func stripMutatingFileSystemPermissions(value any) any {
	fs, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	readOnly := map[string]any{}
	if read, ok := fs["read"]; ok && read != nil {
		readOnly["read"] = read
	}
	return readOnly
}
