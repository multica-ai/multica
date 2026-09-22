package workflow

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	CurrentGraphSchemaVersion = 2
	CurrentPlanVersion        = 1
	MaxWorkflowNodes          = 200
	MaxWorkflowEdges          = 1000
)

// Position is editor state only; execution never uses coordinates.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Assignee struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// FailureAction accepts the compact string used by the editor and the
// structured {"action": ...} form used by design fixtures. It is serialized
// compactly so older clients can continue to round-trip a graph.
type FailureAction string

func (action *FailureAction) UnmarshalJSON(data []byte) error {
	var compact string
	if err := json.Unmarshal(data, &compact); err == nil {
		*action = FailureAction(compact)
		return nil
	}
	var structured struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(data, &structured); err != nil {
		return err
	}
	*action = FailureAction(structured.Action)
	return nil
}

func (action FailureAction) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(action))
}

// Edge carries semantic information when a graph is upgraded to schema v2.
// Empty Kind/ports are the v1 serial-flow defaults.
type Edge struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	Kind       string `json:"kind,omitempty"`
	ScopeID    string `json:"scope_id,omitempty"`
	SourcePort string `json:"source_port,omitempty"`
	TargetPort string `json:"target_port,omitempty"`
	Label      string `json:"label,omitempty"`
}

type Node struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Label        string         `json:"label"`
	Position     Position       `json:"position"`
	AgentID      string         `json:"agent_id,omitempty"` // v1 compatibility
	Instructions string         `json:"instructions,omitempty"`
	InputRefs    []string       `json:"input_refs,omitempty"` // v1 compatibility
	Assignee     *Assignee      `json:"assignee,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	Inputs       map[string]any `json:"inputs,omitempty"`
	Outputs      []OutputField  `json:"outputs,omitempty"`
	OnFailure    FailureAction  `json:"on_failure,omitempty"`
	OnTimeout    FailureAction  `json:"on_timeout,omitempty"`
	MaxRetries   int            `json:"max_retries,omitempty"`
	// UnknownFields keeps forward-compatible node attributes intact when a
	// server reads and writes a graph it cannot fully understand. The client
	// still treats an unknown node type as read-only, but it must not silently
	// delete the node's future fields while displaying it.
	UnknownFields map[string]json.RawMessage `json:"-"`
}

var knownNodeJSONFields = map[string]bool{
	"id": true, "type": true, "label": true, "position": true,
	"agent_id": true, "instructions": true, "input_refs": true,
	"assignee": true, "config": true, "inputs": true, "outputs": true,
	"on_failure": true, "on_timeout": true, "max_retries": true,
}

// UnmarshalJSON preserves fields introduced by a newer workflow engine. This
// is deliberately scoped to nodes because nodes are the extensibility boundary
// exposed to old clients; graph-level structural fields remain strict.
func (n *Node) UnmarshalJSON(data []byte) error {
	type nodeWire Node
	var decoded nodeWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	decoded.UnknownFields = nil
	for key, value := range raw {
		if !knownNodeJSONFields[key] {
			if decoded.UnknownFields == nil {
				decoded.UnknownFields = make(map[string]json.RawMessage)
			}
			decoded.UnknownFields[key] = append(json.RawMessage(nil), value...)
		}
	}
	*n = Node(decoded)
	return nil
}

func (n Node) MarshalJSON() ([]byte, error) {
	type nodeWire Node
	encoded, err := json.Marshal(nodeWire(n))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, err
	}
	for key, value := range n.UnknownFields {
		if !knownNodeJSONFields[key] {
			raw[key] = append(json.RawMessage(nil), value...)
		}
	}
	return json.Marshal(raw)
}

type OutputField struct {
	Key      string `json:"key"`
	Label    string `json:"label,omitempty"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type WorkflowDefaults struct {
	MaxRetries                int `json:"max_retries,omitempty"`
	InitialDelaySeconds       int `json:"initial_delay_seconds,omitempty"`
	BackoffMultiplier         int `json:"backoff_multiplier,omitempty"`
	MaxDelaySeconds           int `json:"max_delay_seconds,omitempty"`
	ExecutionTimeoutSeconds   int `json:"execution_timeout_seconds,omitempty"`
	QueueTimeoutSeconds       int `json:"queue_timeout_seconds,omitempty"`
	HumanTimeoutSeconds       int `json:"human_timeout_seconds,omitempty"`
	RunTimeoutSeconds         int `json:"run_timeout_seconds,omitempty"`
	MaxDispatches             int `json:"max_dispatches,omitempty"`
	MaxActiveExecutionSeconds int `json:"max_active_execution_seconds,omitempty"`
}

// UnmarshalJSON accepts both the flat wire shape used by the API and the
// nested retry object used by the design fixtures. Keeping this compatibility
// at the graph boundary means old drafts remain readable while new clients
// can use the more discoverable defaults.retry form.
func (defaults *WorkflowDefaults) UnmarshalJSON(data []byte) error {
	type retryDefaults struct {
		MaxRetries          *int `json:"max_retries"`
		InitialDelaySeconds *int `json:"initial_delay_seconds"`
		BackoffMultiplier   *int `json:"backoff_multiplier"`
		MaxDelaySeconds     *int `json:"max_delay_seconds"`
	}
	type wire struct {
		MaxRetries                *int           `json:"max_retries"`
		InitialDelaySeconds       *int           `json:"initial_delay_seconds"`
		BackoffMultiplier         *int           `json:"backoff_multiplier"`
		MaxDelaySeconds           *int           `json:"max_delay_seconds"`
		ExecutionTimeoutSeconds   *int           `json:"execution_timeout_seconds"`
		QueueTimeoutSeconds       *int           `json:"queue_timeout_seconds"`
		HumanTimeoutSeconds       *int           `json:"human_timeout_seconds"`
		RunTimeoutSeconds         *int           `json:"run_timeout_seconds"`
		MaxDispatches             *int           `json:"max_dispatches"`
		MaxActiveExecutionSeconds *int           `json:"max_active_execution_seconds"`
		Retry                     *retryDefaults `json:"retry"`
	}
	var value wire
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value.MaxRetries != nil {
		defaults.MaxRetries = *value.MaxRetries
	}
	if value.InitialDelaySeconds != nil {
		defaults.InitialDelaySeconds = *value.InitialDelaySeconds
	}
	if value.BackoffMultiplier != nil {
		defaults.BackoffMultiplier = *value.BackoffMultiplier
	}
	if value.MaxDelaySeconds != nil {
		defaults.MaxDelaySeconds = *value.MaxDelaySeconds
	}
	if value.ExecutionTimeoutSeconds != nil {
		defaults.ExecutionTimeoutSeconds = *value.ExecutionTimeoutSeconds
	}
	if value.QueueTimeoutSeconds != nil {
		defaults.QueueTimeoutSeconds = *value.QueueTimeoutSeconds
	}
	if value.HumanTimeoutSeconds != nil {
		defaults.HumanTimeoutSeconds = *value.HumanTimeoutSeconds
	}
	if value.RunTimeoutSeconds != nil {
		defaults.RunTimeoutSeconds = *value.RunTimeoutSeconds
	}
	if value.MaxDispatches != nil {
		defaults.MaxDispatches = *value.MaxDispatches
	}
	if value.MaxActiveExecutionSeconds != nil {
		defaults.MaxActiveExecutionSeconds = *value.MaxActiveExecutionSeconds
	}
	if value.Retry != nil {
		if value.Retry.MaxRetries != nil {
			defaults.MaxRetries = *value.Retry.MaxRetries
		}
		if value.Retry.InitialDelaySeconds != nil {
			defaults.InitialDelaySeconds = *value.Retry.InitialDelaySeconds
		}
		if value.Retry.BackoffMultiplier != nil {
			defaults.BackoffMultiplier = *value.Retry.BackoffMultiplier
		}
		if value.Retry.MaxDelaySeconds != nil {
			defaults.MaxDelaySeconds = *value.Retry.MaxDelaySeconds
		}
	}
	return nil
}

type Scope struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	EntryNodeID string `json:"entry_node_id,omitempty"`
	ExitNodeID  string `json:"exit_node_id,omitempty"`
	MaxReworks  int    `json:"max_reworks,omitempty"`
}

type Graph struct {
	SchemaVersion int              `json:"schema_version,omitempty"`
	Defaults      WorkflowDefaults `json:"defaults,omitempty"`
	Scopes        []Scope          `json:"scopes,omitempty"`
	Nodes         []Node           `json:"nodes"`
	Edges         []Edge           `json:"edges"`
}

// MarshalJSON keeps collection fields stable at the API and persistence
// boundary. Older workflow snapshots may have nil slices, but the wire
// contract treats nodes and edges as arrays; emitting null makes otherwise
// valid drafts fail the frontend schema before they can be displayed.
func (g Graph) MarshalJSON() ([]byte, error) {
	type graphWire Graph
	nodes := g.Nodes
	if nodes == nil {
		nodes = []Node{}
	}
	edges := g.Edges
	if edges == nil {
		edges = []Edge{}
	}
	return json.Marshal(graphWire{
		SchemaVersion: g.SchemaVersion,
		Defaults:      g.Defaults,
		Scopes:        g.Scopes,
		Nodes:         nodes,
		Edges:         edges,
	})
}

func DefaultGraph() Graph {
	return Graph{
		SchemaVersion: CurrentGraphSchemaVersion,
		Defaults: WorkflowDefaults{
			MaxRetries:                2,
			InitialDelaySeconds:       30,
			BackoffMultiplier:         2,
			MaxDelaySeconds:           900,
			ExecutionTimeoutSeconds:   1800,
			QueueTimeoutSeconds:       600,
			HumanTimeoutSeconds:       259200,
			RunTimeoutSeconds:         2592000,
			MaxDispatches:             1000,
			MaxActiveExecutionSeconds: 86400,
		},
		Nodes: []Node{
			{ID: "start", Type: "start", Label: "Start", Position: Position{X: 80, Y: 160}},
			{ID: "end", Type: "end", Label: "End", Position: Position{X: 600, Y: 160}},
		},
		Edges: []Edge{},
	}
}

var supportedNodeTypes = map[string]bool{
	"start": true, "agent": true, "human_task": true, "human_review": true,
	"condition": true, "parallel": true, "merge": true, "end": true,
}

func IsSupportedNodeType(nodeType string) bool { return supportedNodeTypes[nodeType] }

type ValidationDetail struct {
	Code    string `json:"code"`
	NodeID  string `json:"node_id,omitempty"`
	EdgeID  string `json:"edge_id,omitempty"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// Error is the HTTP-facing error used by the workflow package. Details are
// populated for graph validation so clients can locate every problem in one
// response instead of fixing one opaque error at a time.
type Error struct {
	Status  int
	Message string
	Code    string
	Details []ValidationDetail
}

func (e *Error) Error() string { return e.Message }

func Bad(s string) error      { return &Error{Status: 400, Message: s, Code: "invalid_workflow"} }
func Conflict(s string) error { return &Error{Status: 409, Message: s, Code: "revision_conflict"} }

func validationError(details []ValidationDetail) error {
	if len(details) == 0 {
		return nil
	}
	sort.SliceStable(details, func(i, j int) bool {
		left, right := details[i], details[j]
		if left.NodeID != right.NodeID {
			return left.NodeID < right.NodeID
		}
		if left.EdgeID != right.EdgeID {
			return left.EdgeID < right.EdgeID
		}
		if left.Field != right.Field {
			return left.Field < right.Field
		}
		return left.Code < right.Code
	})
	return &Error{
		Status:  422,
		Code:    "workflow_validation_failed",
		Message: fmt.Sprintf("workflow has %d validation error(s)", len(details)),
		Details: details,
	}
}

func detail(code, message string) ValidationDetail {
	return ValidationDetail{Code: code, Message: message}
}

func nodeDetail(code, nodeID, field, message string) ValidationDetail {
	return ValidationDetail{Code: code, NodeID: nodeID, Field: field, Message: message}
}

func edgeDetail(code, edgeID, message string) ValidationDetail {
	return ValidationDetail{Code: code, EdgeID: edgeID, Message: message}
}

func isFinitePosition(p Position) bool {
	return !math.IsNaN(p.X) && !math.IsInf(p.X, 0) && !math.IsNaN(p.Y) && !math.IsInf(p.Y, 0) && math.Abs(p.X) <= 1e7 && math.Abs(p.Y) <= 1e7
}

func nodeAssignee(n Node) *Assignee {
	if n.Assignee != nil {
		return n.Assignee
	}
	if n.AgentID != "" {
		return &Assignee{Type: "agent", ID: n.AgentID}
	}
	if n.Config != nil {
		if raw, ok := n.Config["assignee"].(map[string]any); ok {
			typ, _ := raw["type"].(string)
			id, _ := raw["id"].(string)
			if typ != "" || id != "" {
				return &Assignee{Type: typ, ID: id}
			}
		}
	}
	return nil
}

func nodeAssigneeID(n Node) string {
	assignee := nodeAssignee(n)
	if assignee == nil || assignee.Type != "agent" {
		return ""
	}
	return assignee.ID
}

func nodeInstructions(n Node) string {
	if strings.TrimSpace(n.Instructions) != "" {
		return n.Instructions
	}
	if n.Config != nil {
		if instructions, ok := n.Config["instructions"].(string); ok {
			return instructions
		}
	}
	return ""
}

func isV2(g Graph) bool { return g.SchemaVersion >= CurrentGraphSchemaVersion }

func requireV2Graph(g Graph) error {
	if graphVersion(g) >= CurrentGraphSchemaVersion {
		return nil
	}
	return &Error{
		Status: 409, Code: "upgrade_required", Message: "upgrade this workflow draft to schema v2 before publishing or running",
		Details: []ValidationDetail{{Code: "upgrade_required", Field: "schema_version", Message: "schema v1 workflows require an explicit upgrade preview and apply step"}},
	}
}

func validateDefaults(defaults WorkflowDefaults) []ValidationDetail {
	details := make([]ValidationDetail, 0)
	if defaults.MaxRetries < 0 || defaults.MaxRetries > 10 {
		details = append(details, detail("invalid_max_retries", "max_retries must be between 0 and 10"))
	}
	if defaults.InitialDelaySeconds < 0 || defaults.InitialDelaySeconds > 86400 {
		details = append(details, detail("invalid_retry_delay", "initial_delay_seconds must be between 0 and 86400"))
	}
	if defaults.BackoffMultiplier < 0 || defaults.BackoffMultiplier > 10 {
		details = append(details, detail("invalid_backoff_multiplier", "backoff_multiplier must be between 0 and 10"))
	}
	if defaults.MaxDelaySeconds < 0 || defaults.MaxDelaySeconds > 86400 || (defaults.MaxDelaySeconds > 0 && defaults.InitialDelaySeconds > 0 && defaults.MaxDelaySeconds < defaults.InitialDelaySeconds) {
		details = append(details, detail("invalid_max_delay", "max_delay_seconds must be at least the initial delay and at most 86400"))
	}
	if defaults.ExecutionTimeoutSeconds < 0 || defaults.ExecutionTimeoutSeconds > 86400 {
		details = append(details, detail("invalid_execution_timeout", "execution_timeout_seconds must be between 0 and 86400"))
	}
	if defaults.QueueTimeoutSeconds < 0 || defaults.QueueTimeoutSeconds > 86400 {
		details = append(details, detail("invalid_queue_timeout", "queue_timeout_seconds must be between 0 and 86400"))
	}
	if defaults.HumanTimeoutSeconds < 0 || defaults.HumanTimeoutSeconds > 604800*10 {
		details = append(details, detail("invalid_human_timeout", "human_timeout_seconds must be between 0 and 6048000"))
	}
	if defaults.RunTimeoutSeconds < 0 || defaults.RunTimeoutSeconds > 604800*31 {
		details = append(details, detail("invalid_run_timeout", "run_timeout_seconds must be between 0 and 2678400"))
	}
	if defaults.MaxDispatches < 0 || defaults.MaxDispatches > 1000000 {
		details = append(details, detail("invalid_dispatch_budget", "max_dispatches must be between 0 and 1000000"))
	}
	if defaults.MaxActiveExecutionSeconds < 0 || defaults.MaxActiveExecutionSeconds > 604800*31 {
		details = append(details, detail("invalid_active_execution_budget", "max_active_execution_seconds must be between 0 and 2678400"))
	}
	return details
}

func collectOutputReferences(value any, refs map[string]bool) {
	switch current := value.(type) {
	case map[string]any:
		if kind, _ := current["kind"].(string); kind == "output" {
			if nodeID, _ := current["node_id"].(string); nodeID != "" {
				refs[nodeID] = true
			}
		}
		for _, nested := range current {
			collectOutputReferences(nested, refs)
		}
	case []any:
		for _, nested := range current {
			collectOutputReferences(nested, refs)
		}
	case []map[string]any:
		for _, nested := range current {
			collectOutputReferences(nested, refs)
		}
	}
}

func reachableNodes(root string, adjacency map[string][]string) map[string]bool {
	seen := make(map[string]bool)
	var walk func(string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, next := range adjacency[id] {
			walk(next)
		}
	}
	walk(root)
	return seen
}

// Validate permits incomplete drafts but rejects malformed graph structure.
// When runnable is true it additionally enforces the executable W1 contract.
// V1 graphs remain readable so an existing draft can be explicitly upgraded.
func Validate(g Graph, runnable bool) error {
	details := make([]ValidationDetail, 0)
	details = append(details, validateDefaults(g.Defaults)...)
	if len(g.Nodes) > MaxWorkflowNodes {
		details = append(details, detail("node_limit_exceeded", "workflow cannot contain more than 200 nodes"))
	}
	if len(g.Edges) > MaxWorkflowEdges {
		details = append(details, detail("edge_limit_exceeded", "workflow cannot contain more than 1000 edges"))
	}
	if g.SchemaVersion < 0 || g.SchemaVersion > CurrentGraphSchemaVersion {
		details = append(details, detail("unsupported_graph_version", "this workflow graph version is not supported"))
	}

	nodes := make(map[string]Node, len(g.Nodes))
	startIDs, endIDs := []string{}, []string{}
	for _, n := range g.Nodes {
		if n.ID == "" || len(n.ID) > 100 || strings.ContainsAny(n.ID, "/\\") {
			details = append(details, nodeDetail("invalid_node_id", n.ID, "id", "node id is required and cannot contain a slash"))
			continue
		}
		if _, exists := nodes[n.ID]; exists {
			details = append(details, nodeDetail("duplicate_node_id", n.ID, "id", "node id must be unique"))
			continue
		}
		nodes[n.ID] = n
		if !isFinitePosition(n.Position) {
			details = append(details, nodeDetail("invalid_position", n.ID, "position", "node position must be a finite coordinate"))
		}
		if !supportedNodeTypes[n.Type] {
			if runnable {
				details = append(details, nodeDetail("unsupported_node_type", n.ID, "type", "node type is not supported"))
			}
			continue
		}
		if n.MaxRetries < 0 || n.MaxRetries > 10 {
			details = append(details, nodeDetail("invalid_max_retries", n.ID, "max_retries", "max_retries must be between 0 and 10"))
		}
		seenOutputs := map[string]bool{}
		for _, output := range n.Outputs {
			if strings.TrimSpace(output.Key) == "" || seenOutputs[output.Key] {
				details = append(details, nodeDetail("invalid_output_field", n.ID, "outputs", "output keys must be non-empty and unique"))
				continue
			}
			seenOutputs[output.Key] = true
			if !validOutputTypeName(output.Type) {
				details = append(details, nodeDetail("invalid_output_type", n.ID, "outputs."+output.Key+".type", "output type is not supported"))
			}
		}
		switch n.Type {
		case "start":
			startIDs = append(startIDs, n.ID)
			_, inputDetails := startInputFields(n)
			details = append(details, inputDetails...)
		case "end":
			endIDs = append(endIDs, n.ID)
		case "agent":
			if runnable {
				a := nodeAssignee(n)
				if a == nil || a.Type != "agent" || strings.TrimSpace(a.ID) == "" {
					details = append(details, nodeDetail("assignee_required", n.ID, "config.assignee", "select an agent for this step"))
				}
				if strings.TrimSpace(nodeInstructions(n)) == "" {
					details = append(details, nodeDetail("instructions_required", n.ID, "config.instructions", "instructions are required"))
				}
			}
		case "human_task", "human_review":
			if runnable {
				a := nodeAssignee(n)
				if a == nil || a.Type != "member" || strings.TrimSpace(a.ID) == "" {
					details = append(details, nodeDetail("assignee_required", n.ID, "config.assignee", "select a workspace member for this step"))
				}
			}
		case "condition":
			if runnable {
				details = append(details, validateConditionNode(n, g.Edges)...)
			}
		}
	}

	if len(startIDs) > 1 {
		details = append(details, detail("multiple_start_nodes", "workflow must contain only one Start node"))
	}
	if len(endIDs) > 1 {
		details = append(details, detail("multiple_end_nodes", "workflow must contain only one End node"))
	}
	if runnable {
		if len(startIDs) != 1 {
			details = append(details, detail("start_required", "workflow must contain exactly one Start node"))
		}
		if len(endIDs) != 1 {
			details = append(details, detail("end_required", "workflow must contain exactly one End node"))
		}
	}

	edges := make(map[string]bool, len(g.Edges))
	connections := make(map[string]bool, len(g.Edges))
	incoming := make(map[string][]string, len(nodes))
	outgoing := make(map[string][]string, len(nodes))
	for _, e := range g.Edges {
		if e.ID == "" || edges[e.ID] {
			details = append(details, edgeDetail("duplicate_edge_id", e.ID, "edge id must be unique"))
			continue
		}
		edges[e.ID] = true
		key := e.Source + "\x00" + e.Target + "\x00" + e.SourcePort + "\x00" + e.TargetPort
		if connections[key] {
			details = append(details, edgeDetail("duplicate_edge", e.ID, "edge connection must be unique"))
		}
		connections[key] = true
		if _, ok := nodes[e.Source]; !ok {
			details = append(details, edgeDetail("missing_edge_source", e.ID, "edge source does not exist"))
			continue
		}
		if _, ok := nodes[e.Target]; !ok {
			details = append(details, edgeDetail("missing_edge_target", e.ID, "edge target does not exist"))
			continue
		}
		if e.Source == e.Target {
			details = append(details, edgeDetail("self_edge", e.ID, "self edges are not allowed"))
			continue
		}
		if e.Kind != "" && e.Kind != "flow" && e.Kind != "rework" && e.Kind != "compensation" {
			details = append(details, edgeDetail("unsupported_edge_kind", e.ID, "edge kind is not supported"))
		}
		if e.Kind == "rework" {
			if nodes[e.Source].Type != "human_review" || nodes[e.Target].Type == "start" || nodes[e.Target].Type == "end" {
				details = append(details, edgeDetail("invalid_rework_edge", e.ID, "rework edges must leave a human review and target an executable step"))
			}
			if e.ScopeID == "" {
				details = append(details, edgeDetail("rework_scope_required", e.ID, "rework edges must reference a declared scope"))
			}
		}
		if e.Kind != "rework" && e.Kind != "compensation" {
			incoming[e.Target] = append(incoming[e.Target], e.Source)
			outgoing[e.Source] = append(outgoing[e.Source], e.Target)
		}
	}
	if len(details) > 0 {
		return validationError(details)
	}
	scopes := make(map[string]Scope, len(g.Scopes))
	for _, scope := range g.Scopes {
		if scope.ID == "" || scopes[scope.ID] != (Scope{}) {
			details = append(details, detail("invalid_rework_scope", "rework scope ids must be non-empty and unique"))
			continue
		}
		scopes[scope.ID] = scope
		if scope.Type != "rework" {
			details = append(details, detail("unsupported_scope_type", "only rework scopes are supported"))
		}
		if scope.MaxReworks < 0 || scope.MaxReworks > 10 {
			details = append(details, detail("invalid_rework_limit", "rework scope max_reworks must be between 0 and 10"))
		}
		if scope.EntryNodeID != "" && nodes[scope.EntryNodeID].ID == "" {
			details = append(details, detail("missing_scope_entry", "rework scope entry node does not exist"))
		}
		if scope.ExitNodeID != "" && nodes[scope.ExitNodeID].ID == "" {
			details = append(details, detail("missing_scope_exit", "rework scope exit node does not exist"))
		}
	}
	for _, edge := range g.Edges {
		if edge.Kind == "rework" && scopes[edge.ScopeID].ID == "" {
			details = append(details, edgeDetail("rework_scope_not_found", edge.ID, "rework edge references an unknown scope"))
			continue
		}
		if edge.Kind == "rework" && runnable {
			scope := scopes[edge.ScopeID]
			if scope.EntryNodeID == "" || scope.ExitNodeID == "" {
				details = append(details, edgeDetail("rework_scope_bounds_required", edge.ID, "rework scope must declare entry and exit nodes"))
				continue
			}
			if scope.MaxReworks <= 0 {
				details = append(details, edgeDetail("rework_disabled", edge.ID, "rework scope must allow at least one rework"))
			}
			if edge.Source != scope.ExitNodeID || edge.Target != scope.EntryNodeID {
				details = append(details, edgeDetail("rework_scope_mismatch", edge.ID, "rework edge must return from the scope exit to its entry"))
			}
		}
	}
	if len(details) > 0 {
		return validationError(details)
	}
	if !runnable {
		return validationError(details)
	}
	startID, endID := startIDs[0], endIDs[0]
	if len(incoming[startID]) > 0 {
		details = append(details, nodeDetail("start_has_incoming", startID, "edges", "Start cannot have incoming edges"))
	}
	if len(outgoing[endID]) > 0 {
		details = append(details, nodeDetail("end_has_outgoing", endID, "edges", "End cannot have outgoing edges"))
	}

	for id, n := range nodes {
		if n.Type == "parallel" && len(outgoing[id]) < 2 {
			details = append(details, nodeDetail("parallel_branches_required", id, "edges", "parallel step needs at least two branches"))
		}
		if n.Type == "parallel" && len(outgoing[id]) > 10 {
			details = append(details, nodeDetail("parallel_branch_limit_exceeded", id, "edges", "parallel step cannot contain more than ten branches"))
		}
		if isV2(g) && (n.Type == "agent" || n.Type == "human_task" || n.Type == "human_review") {
			normalOutputs := 0
			for _, edge := range g.Edges {
				if edge.Source == id && normalFlowEdge(edge) && !isFailureEdge(edge) {
					normalOutputs++
				}
			}
			if normalOutputs > 1 {
				details = append(details, nodeDetail("multiple_normal_outputs", id, "edges", "use a condition or parallel step for multiple outputs"))
			}
		}
		if n.Type == "merge" && len(incoming[id]) < 2 {
			details = append(details, nodeDetail("merge_branches_required", id, "edges", "merge step needs at least two incoming branches"))
		}
	}
	for id, n := range nodes {
		if n.Type != "parallel" || len(outgoing[id]) < 2 {
			continue
		}
		common := map[string]bool{}
		first := true
		for _, branch := range outgoing[id] {
			reachableFromBranch := reachableNodes(branch, outgoing)
			if first {
				for candidate := range reachableFromBranch {
					if nodes[candidate].Type == "merge" {
						common[candidate] = true
					}
				}
				first = false
				continue
			}
			for candidate := range common {
				if !reachableFromBranch[candidate] {
					delete(common, candidate)
				}
			}
		}
		if len(common) == 0 {
			details = append(details, nodeDetail("parallel_merge_required", id, "edges", "parallel branches must converge at a merge step"))
		}
	}

	colors := make(map[string]int, len(nodes))
	var visit func(string) bool
	visit = func(id string) bool {
		switch colors[id] {
		case 1:
			return false
		case 2:
			return true
		}
		colors[id] = 1
		for _, next := range outgoing[id] {
			if !visit(next) {
				return false
			}
		}
		colors[id] = 2
		return true
	}
	for id := range nodes {
		if !visit(id) {
			details = append(details, detail("cycle", "workflow contains a cycle"))
			break
		}
	}

	fromStart, toEnd := reachableNodes(startID, outgoing), reachableNodes(endID, incoming)
	for id, n := range nodes {
		if !fromStart[id] || !toEnd[id] {
			details = append(details, nodeDetail("disconnected", id, "edges", "every node must be on a Start-to-End path"))
		}
		ancestors := reachableNodes(id, incoming)
		refs := make(map[string]bool, len(n.InputRefs))
		for _, ref := range n.InputRefs {
			refs[ref] = true
		}
		collectOutputReferences(n.Inputs, refs)
		for ref := range refs {
			if ref == id || !ancestors[ref] || (nodes[ref].Type != "agent" && nodes[ref].Type != "human_task" && nodes[ref].Type != "human_review") {
				details = append(details, nodeDetail("invalid_reference", id, "input_refs", fmt.Sprintf("%s is not an available agent output", ref)))
			}
		}
	}
	return validationError(details)
}

func conditionHasDefault(n Node, edges []Edge) bool {
	if n.Config != nil {
		if value, ok := n.Config["has_default"].(bool); ok && value {
			return true
		}
		if branches, ok := n.Config["branches"].([]any); ok {
			for _, raw := range branches {
				if branch, ok := raw.(map[string]any); ok {
					if defaultBranch, ok := branch["default"].(bool); ok && defaultBranch {
						return true
					}
				}
			}
		}
		if branches, ok := n.Config["branches"].([]map[string]any); ok {
			for _, branch := range branches {
				if defaultBranch, ok := branch["default"].(bool); ok && defaultBranch {
					return true
				}
			}
		}
	}
	for _, edge := range edges {
		if normalFlowEdge(edge) && edge.Source == n.ID && edge.SourcePort == "default" {
			return true
		}
	}
	return false
}

func validateConditionNode(node Node, edges []Edge) []ValidationDetail {
	details := make([]ValidationDetail, 0)
	branches := conditionObjects(nil)
	if node.Config != nil {
		if raw, exists := node.Config["branches"]; exists {
			branches = conditionObjects(raw)
			if len(branches) == 0 {
				details = append(details, nodeDetail("invalid_condition_branches", node.ID, "config.branches", "condition branches must be a non-empty array"))
			}
		}
	}
	defaultCount := 0
	seenIDs := map[string]bool{}
	for _, branch := range branches {
		branchID, _ := branch["id"].(string)
		branchID = strings.TrimSpace(branchID)
		if branchID == "" || seenIDs[branchID] {
			details = append(details, nodeDetail("invalid_condition_branch", node.ID, "config.branches", "condition branch ids must be non-empty and unique"))
			continue
		}
		seenIDs[branchID] = true
		isDefault, _ := branch["default"].(bool)
		if isDefault {
			defaultCount++
		}
		port, _ := branch["port"].(string)
		if strings.TrimSpace(port) == "" {
			port = branchID
		}
		connected := false
		for _, edge := range edges {
			if normalFlowEdge(edge) && edge.Source == node.ID && edge.SourcePort == port {
				connected = true
				break
			}
		}
		if !connected {
			details = append(details, nodeDetail("condition_branch_unconnected", node.ID, "config.branches."+branchID, "condition branch must connect to an outgoing edge"))
		}
	}
	if len(branches) > 0 {
		if defaultCount != 1 {
			details = append(details, nodeDetail("condition_default_required", node.ID, "config.branches", "a condition must have exactly one default branch"))
		}
	} else if !conditionHasDefault(node, edges) {
		details = append(details, nodeDetail("condition_default_required", node.ID, "config.branches", "a condition must have one default branch"))
	}
	return details
}

type CompiledNode struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Index       int       `json:"index"`
	Assignee    *Assignee `json:"assignee,omitempty"`
	SuccessNext []string  `json:"success_next,omitempty"`
	FailureNext []string  `json:"failure_next,omitempty"`
}

type CompiledPlan struct {
	Version      int            `json:"plan_version"`
	GraphVersion int            `json:"graph_schema_version"`
	StartNodeID  string         `json:"start_node_id"`
	EndNodeID    string         `json:"end_node_id"`
	Nodes        []CompiledNode `json:"nodes"`
	Edges        []Edge         `json:"edges"`
}

// Compile is the server-side executable boundary. The returned plan contains
// only stable node IDs and semantic edges, so a run can keep it as an
// immutable snapshot even after a draft is edited.
func Compile(g Graph) (CompiledPlan, error) {
	if err := Validate(g, true); err != nil {
		return CompiledPlan{}, err
	}
	plan := CompiledPlan{Version: CurrentPlanVersion, GraphVersion: g.SchemaVersion, Edges: append([]Edge(nil), g.Edges...)}
	if plan.GraphVersion == 0 {
		plan.GraphVersion = 1
	}
	for index, n := range g.Nodes {
		compiled := CompiledNode{ID: n.ID, Type: n.Type, Index: index, Assignee: nodeAssignee(n)}
		for _, edge := range g.Edges {
			if edge.Source != n.ID {
				continue
			}
			if strings.HasPrefix(edge.SourcePort, "failure") || edge.Kind == "compensation" {
				compiled.FailureNext = append(compiled.FailureNext, edge.Target)
			} else {
				compiled.SuccessNext = append(compiled.SuccessNext, edge.Target)
			}
		}
		plan.Nodes = append(plan.Nodes, compiled)
		if n.Type == "start" {
			plan.StartNodeID = n.ID
		}
		if n.Type == "end" {
			plan.EndNodeID = n.ID
		}
	}
	return plan, nil
}
