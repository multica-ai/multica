package dagcore

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Operation string

const (
	OperationCreate          Operation = "create"
	OperationUpdate          Operation = "update"
	OperationDelete          Operation = "delete"
	OperationLink            Operation = "link"
	OperationUnlink          Operation = "unlink"
	OperationAssert          Operation = "assert"
	OperationCite            Operation = "cite"
	OperationConflict        Operation = "conflict"
	OperationResolveConflict Operation = "resolve_conflict"
)

type Dot struct {
	AgentID string `json:"agent_id"`
	Counter int64  `json:"counter"`
}

type DVT struct {
	Dot     Dot              `json:"dot"`
	Context map[string]int64 `json:"context"`
}

type DVTOrder string

const (
	DVTBefore     DVTOrder = "before"
	DVTAfter      DVTOrder = "after"
	DVTEqual      DVTOrder = "equal"
	DVTConcurrent DVTOrder = "concurrent"
)

type Event struct {
	ID        string         `json:"id"`
	RecordIDs []string       `json:"record_ids"`
	AgentID   string         `json:"agent_id"`
	DVT       DVT            `json:"dvt"`
	Operation Operation      `json:"operation"`
	Payload   map[string]any `json:"payload"`
	Reason    string         `json:"reason"`
}

type Link struct {
	FromID  string `json:"from_id"`
	ToID    string `json:"to_id"`
	Type    string `json:"type"`
	EventID string `json:"event_id"`
}

type Fact struct {
	ID         string   `json:"id"`
	Predicate  string   `json:"predicate"`
	Args       []string `json:"args"`
	EventID    string   `json:"event_id"`
	GroundedBy []string `json:"grounded_by"`
	Confidence *float64 `json:"confidence,omitempty"`
}

type Schema struct {
	Name      string   `json:"name"`
	DependsOn []string `json:"depends_on"`
}

type Citation struct {
	CitationID  string   `json:"citation_id"`
	SourceID    string   `json:"source_id"`
	Probability *float64 `json:"probability,omitempty"`
}

type CitationChain struct {
	AssertionID string     `json:"assertion_id"`
	Citations   []Citation `json:"citations"`
}

type MissingInverse struct {
	Link        Link   `json:"link"`
	InverseType string `json:"inverse_type"`
}

type ConflictState struct {
	LeftFactID  string `json:"left_fact_id"`
	RightFactID string `json:"right_fact_id"`
	Predicate   string `json:"predicate"`
	Severity    string `json:"severity"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
}

type FieldWrite struct {
	EventID  string `json:"event_id"`
	RecordID string `json:"record_id"`
	Field    string `json:"field"`
	Value    string `json:"value"`
	DVT      DVT    `json:"dvt"`
}

func ValidateEvent(e Event) error {
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("event id is required")
	}
	if len(e.RecordIDs) == 0 {
		return errors.New("event must affect at least one record")
	}
	for _, id := range e.RecordIDs {
		if strings.TrimSpace(id) == "" {
			return errors.New("record ids must not be empty")
		}
	}
	if strings.TrimSpace(e.AgentID) == "" {
		return errors.New("agent id is required")
	}
	if e.DVT.Dot.AgentID != e.AgentID {
		return fmt.Errorf("dvt dot agent %q does not match event agent %q", e.DVT.Dot.AgentID, e.AgentID)
	}
	if e.DVT.Dot.Counter <= 0 {
		return errors.New("dvt dot counter must be positive")
	}
	if _, ok := validOperations[e.Operation]; !ok {
		return fmt.Errorf("unsupported operation %q", e.Operation)
	}
	return nil
}

var validOperations = map[Operation]struct{}{
	OperationCreate: {}, OperationUpdate: {}, OperationDelete: {}, OperationLink: {},
	OperationUnlink: {}, OperationAssert: {}, OperationCite: {}, OperationConflict: {},
	OperationResolveConflict: {},
}

func Increment(agentID string, context map[string]int64) (DVT, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return DVT{}, errors.New("agent id is required")
	}
	ctx := cloneContext(context)
	counter := ctx[agentID] + 1
	ctx[agentID] = counter
	return DVT{Dot: Dot{AgentID: agentID, Counter: counter}, Context: ctx}, nil
}

func Merge(a, b DVT) DVT {
	merged := vector(a)
	for agent, counter := range vector(b) {
		if counter > merged[agent] {
			merged[agent] = counter
		}
	}
	agentID := a.Dot.AgentID
	if agentID == "" {
		agentID = b.Dot.AgentID
	}
	return DVT{Dot: Dot{AgentID: agentID, Counter: merged[agentID]}, Context: merged}
}

func Compare(a, b DVT) DVTOrder {
	av := vector(a)
	bv := vector(b)
	agents := map[string]struct{}{}
	for agent := range av {
		agents[agent] = struct{}{}
	}
	for agent := range bv {
		agents[agent] = struct{}{}
	}
	less, greater := false, false
	for agent := range agents {
		ac, bc := av[agent], bv[agent]
		if ac < bc {
			less = true
		}
		if ac > bc {
			greater = true
		}
	}
	switch {
	case !less && !greater:
		return DVTEqual
	case less && !greater:
		return DVTBefore
	case greater && !less:
		return DVTAfter
	default:
		return DVTConcurrent
	}
}

func MissingInverseLinks(links []Link, inverseTypes map[string]string) []MissingInverse {
	active := make(map[string]struct{}, len(links))
	for _, link := range links {
		active[linkKey(link.FromID, link.ToID, link.Type)] = struct{}{}
	}
	missing := make([]MissingInverse, 0)
	for _, link := range links {
		inverseType, ok := inverseTypes[link.Type]
		if !ok || inverseType == "" {
			continue
		}
		if _, ok := active[linkKey(link.ToID, link.FromID, inverseType)]; !ok {
			missing = append(missing, MissingInverse{Link: link, InverseType: inverseType})
		}
	}
	return missing
}

func ValidateAcyclicSchemas(schemas []Schema) error {
	graph := make(map[string][]string, len(schemas))
	known := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		name := strings.TrimSpace(schema.Name)
		if name == "" {
			return errors.New("schema name is required")
		}
		known[name] = struct{}{}
		graph[name] = append([]string(nil), schema.DependsOn...)
	}
	for name, deps := range graph {
		for _, dep := range deps {
			if _, ok := known[dep]; !ok {
				return fmt.Errorf("schema %q depends on unknown type %q", name, dep)
			}
		}
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var walk func(string, []string) error
	walk = func(name string, stack []string) error {
		if visiting[name] {
			return fmt.Errorf("schema dependency cycle: %s -> %s", strings.Join(stack, " -> "), name)
		}
		if visited[name] {
			return nil
		}
		visiting[name] = true
		for _, dep := range graph[name] {
			if err := walk(dep, append(stack, name)); err != nil {
				return err
			}
		}
		visiting[name] = false
		visited[name] = true
		return nil
	}
	for name := range graph {
		if err := walk(name, nil); err != nil {
			return err
		}
	}
	return nil
}

func SortFacts(facts []Fact) []Fact {
	out := append([]Fact(nil), facts...)
	sort.SliceStable(out, func(i, j int) bool {
		return factSortKey(out[i]) < factSortKey(out[j])
	})
	return out
}

func DetectContradictions(facts []Fact) []ConflictState {
	groundedAssertions := make(map[string][]Fact)
	for _, fact := range facts {
		if fact.Predicate != "asserts" || len(fact.Args) < 3 || len(fact.GroundedBy) == 0 {
			continue
		}
		value := strings.ToLower(fact.Args[2])
		if value != "true" && value != "false" {
			continue
		}
		key := fact.Args[0] + "\x00" + fact.Args[1]
		groundedAssertions[key] = append(groundedAssertions[key], fact)
	}
	conflicts := make([]ConflictState, 0)
	for _, group := range groundedAssertions {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				if strings.EqualFold(group[i].Args[2], group[j].Args[2]) {
					continue
				}
				conflicts = append(conflicts, ConflictState{
					LeftFactID:  group[i].ID,
					RightFactID: group[j].ID,
					Predicate:   group[i].Args[1],
					Severity:    "requires_review",
					Status:      "open",
					Reason:      "grounded contradiction",
				})
			}
		}
	}
	return conflicts
}

func ValidateCitationChain(chain CitationChain) error {
	if strings.TrimSpace(chain.AssertionID) == "" {
		return errors.New("assertion_id is required")
	}
	for i, citation := range chain.Citations {
		if strings.TrimSpace(citation.CitationID) == "" {
			return fmt.Errorf("citation %d missing citation_id", i)
		}
		if strings.TrimSpace(citation.SourceID) == "" {
			return fmt.Errorf("citation %d missing source_id", i)
		}
		if citation.Probability != nil && (*citation.Probability < 0 || *citation.Probability > 1) {
			return fmt.Errorf("citation %d probability outside [0,1]", i)
		}
	}
	return nil
}

func ConcurrentFieldConflicts(writes []FieldWrite) []ConflictState {
	conflicts := make([]ConflictState, 0)
	for i := 0; i < len(writes); i++ {
		for j := i + 1; j < len(writes); j++ {
			left, right := writes[i], writes[j]
			if left.RecordID == "" || left.RecordID != right.RecordID || left.Field == "" || left.Field != right.Field {
				continue
			}
			if left.Value == right.Value || Compare(left.DVT, right.DVT) != DVTConcurrent {
				continue
			}
			conflicts = append(conflicts, ConflictState{
				LeftFactID:  left.EventID,
				RightFactID: right.EventID,
				Predicate:   left.Field,
				Severity:    "requires_review",
				Status:      "open",
				Reason:      "concurrent single-field write",
			})
		}
	}
	return conflicts
}

func cloneContext(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func vector(d DVT) map[string]int64 {
	out := cloneContext(d.Context)
	if d.Dot.AgentID != "" && d.Dot.Counter > out[d.Dot.AgentID] {
		out[d.Dot.AgentID] = d.Dot.Counter
	}
	return out
}

func linkKey(fromID, toID, typ string) string {
	return fromID + "\x00" + toID + "\x00" + typ
}

func factSortKey(f Fact) string {
	return f.Predicate + "\x00" + strings.Join(f.Args, "\x00") + "\x00" + f.EventID + "\x00" + f.ID
}
