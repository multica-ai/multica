// Package kagentbroker is a DEV security prototype, NOT READY FOR DEPLOYMENT.
// Independent review and TLS/network qualification are required. In particular,
// alpha7 includes built-in ask_user even without configured tools; this package
// must never be described as providing a zero-tool runtime.
package kagentbroker

import (
	"regexp"
	"strings"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var namePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

const managedBy = "app.kubernetes.io/managed-by"
const agentIDLabel = "multica.io/agent-id"
const ownerAnnotation = "multica.io/owner-token"
const nameAnnotation = "multica.io/agent-name"
const revisionAnnotation = "multica.io/desired-revision"

// Policy is copied at construction; there is no caller-controlled enrollment,
// adoption or policy reload. The broker journal alone establishes a new UID.
type Policy struct {
	Namespace, AgentID, AgentName                       string
	Harness, Model                                      string
	ExpectedUID, OwnerToken                             string
	Audience, ServiceAccountUsername, ServiceAccountUID string
}

func (p Policy) valid() bool {
	parts := strings.Split(p.ServiceAccountUsername, ":")
	return namePattern.MatchString(p.Namespace) && uuidPattern.MatchString(p.AgentID) &&
		len(p.AgentName) > 0 && len(p.AgentName) <= 256 && namePattern.MatchString(p.Harness) && namePattern.MatchString(p.Model) &&
		(p.ExpectedUID == "" || uuidPattern.MatchString(p.ExpectedUID)) && uuidPattern.MatchString(p.OwnerToken) &&
		tokenSafe(p.Audience) && tokenSafe(p.ServiceAccountUID) && len(parts) == 4 && parts[0] == "system" && parts[1] == "serviceaccount" && namePattern.MatchString(parts[2]) && namePattern.MatchString(parts[3])
}
func (p Policy) name() string { return "multica-agent-" + p.AgentID }
func (p Policy) collection() string {
	return "/apis/api.kagent.dev/v1alpha3/namespaces/" + p.Namespace + "/agents"
}
func (p Policy) resource() string { return p.collection() + "/" + p.name() }
func (p Policy) a2a() string      { return "/agents/" + p.Namespace + "/" + p.name() }
func reference(v any, name string) bool {
	m := object(v)
	return keys(m, []string{"name"}) && m["name"] == name
}
func optionalText(m map[string]any, k string, nullable bool) bool {
	v, ok := m[k]
	if !ok {
		return true
	}
	if nullable && v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && len(s) <= 16384 && !strings.ContainsRune(s, 0)
}
func (p Policy) metadata(m map[string]any, mode string) bool {
	required := []string{"labels", "annotations"}
	switch mode {
	case "create":
		required = append(required, "name", "namespace")
	case "patch":
		required = append(required, "uid", "resourceVersion")
	case "observed":
		required = append(required, "name", "namespace", "uid", "resourceVersion")
	default:
		return false
	}
	optional := []string{}
	if mode == "observed" {
		optional = []string{"generation", "creationTimestamp", "managedFields"}
	}
	if !keys(m, required, optional...) {
		return false
	}
	if mode != "patch" && (m["name"] != p.name() || m["namespace"] != p.Namespace) {
		return false
	}
	if mode != "create" && (m["uid"] != p.ExpectedUID || len(str(m["resourceVersion"])) == 0 || len(str(m["resourceVersion"])) > 256) {
		return false
	}
	l, a := object(m["labels"]), object(m["annotations"])
	return keys(l, []string{managedBy, agentIDLabel}) && l[managedBy] == "multica" && l[agentIDLabel] == p.AgentID &&
		keys(a, []string{ownerAnnotation, nameAnnotation, revisionAnnotation}) && a[ownerAnnotation] == p.OwnerToken && a[nameAnnotation] == p.AgentName && len(str(a[revisionAnnotation])) > 0 && len(str(a[revisionAnnotation])) <= 128
}
func (p Policy) spec(m map[string]any, patch bool) bool {
	if !keys(m, []string{"harnessRef", "template"}, "harness", "templateRef") || !reference(m["harnessRef"], p.Harness) {
		return false
	}
	for _, k := range []string{"harness", "templateRef"} {
		v, exists := m[k]
		if (exists && v != nil) || (patch && !exists) {
			return false
		}
	}
	t := object(m["template"])
	if !keys(t, []string{"modelConfig"}, "description", "systemPrompt", "systemPromptFrom") || !reference(t["modelConfig"], p.Model) || !optionalText(t, "description", patch) || !optionalText(t, "systemPrompt", patch) {
		return false
	}
	// No ConfigMap prompt references. The real merge patch clears this leaf.
	v, exists := t["systemPromptFrom"]
	if exists && v != nil {
		return false
	}
	if patch {
		for _, k := range []string{"description", "systemPrompt", "systemPromptFrom"} {
			if _, exists := t[k]; !exists {
				return false
			}
		}
	}
	return true
}
func (p Policy) agent(m map[string]any, mode string) bool {
	if mode == "patch" {
		return keys(m, []string{"metadata", "spec"}) && p.metadata(object(m["metadata"]), mode) && p.spec(object(m["spec"]), true)
	}
	if !keys(m, []string{"apiVersion", "kind", "metadata", "spec"}, "status") || m["apiVersion"] != "api.kagent.dev/v1alpha3" || m["kind"] != "Agent" || !p.metadata(object(m["metadata"]), mode) || !p.spec(object(m["spec"]), false) {
		return false
	}
	if status, exists := m["status"]; exists {
		s := object(status)
		if s == nil || (mode == "create" && len(s) != 0) {
			return false
		}
	}
	return true
}
func (p Policy) deletion(m map[string]any) bool {
	pre := object(m["preconditions"])
	return keys(m, []string{"apiVersion", "kind", "preconditions"}) && m["apiVersion"] == "v1" && m["kind"] == "DeleteOptions" && keys(pre, []string{"uid", "resourceVersion"}) && pre["uid"] == p.ExpectedUID && len(str(pre["resourceVersion"])) > 0 && len(str(pre["resourceVersion"])) <= 256
}

// rpc recreates a small alpha7 request from validated leaves, never a raw body.
func rpc(m map[string]any) (map[string]any, error) {
	if !keys(m, []string{"jsonrpc", "id", "method", "params"}) || m["jsonrpc"] != "2.0" || !uuidPattern.MatchString(str(m["id"])) {
		return nil, errDenied
	}
	params := object(m["params"])
	var out any
	switch m["method"] {
	case "SendMessage":
		if !keys(params, []string{"message", "configuration"}) {
			return nil, errDenied
		}
		msg, c := object(params["message"]), object(params["configuration"])
		if !keys(msg, []string{"messageId", "role", "parts"}, "contextId") || !uuidPattern.MatchString(str(msg["messageId"])) || msg["role"] != "ROLE_USER" {
			return nil, errDenied
		}
		if v, exists := msg["contextId"]; exists && !uuidPattern.MatchString(str(v)) {
			return nil, errDenied
		}
		parts, ok := msg["parts"].([]any)
		if !ok || len(parts) != 1 {
			return nil, errDenied
		}
		part := object(parts[0])
		text, ok := part["text"].(string)
		if !keys(part, []string{"text"}) || !ok || len(text) == 0 || len(text) > 16384 || strings.ContainsRune(text, 0) {
			return nil, errDenied
		}
		modes, ok := c["acceptedOutputModes"].([]any)
		if !keys(c, []string{"returnImmediately", "acceptedOutputModes"}) || c["returnImmediately"] != true || !ok || len(modes) != 1 || modes[0] != "text/plain" {
			return nil, errDenied
		}
		message := map[string]any{"messageId": str(msg["messageId"]), "role": "ROLE_USER", "parts": []any{map[string]any{"text": text}}}
		if v, exists := msg["contextId"]; exists {
			message["contextId"] = str(v)
		}
		out = map[string]any{"message": message, "configuration": map[string]any{"returnImmediately": true, "acceptedOutputModes": []string{"text/plain"}}}
	case "GetTask", "CancelTask":
		if !keys(params, []string{"id"}) || !uuidPattern.MatchString(str(params["id"])) {
			return nil, errDenied
		}
		out = map[string]any{"id": str(params["id"])}
	default:
		return nil, errDenied
	}
	return map[string]any{"jsonrpc": "2.0", "id": str(m["id"]), "method": str(m["method"]), "params": out}, nil
}
