package accessdecision

import "sort"

// Report is the stable aggregate used to inspect shadow drift.
type Report struct {
	Total  int
	Diffs  int
	Groups []Group
}

// Group aggregates calls for one agent, runtime, and canonical tool. When a
// tool is unknown, Tool falls back to the observed Gateway name so the gap is
// still visible instead of disappearing from the report.
type Group struct {
	AgentID        string
	RuntimeID      string
	Tool           string
	PolicyDecision PolicyDecision
	Total          int
	Diffs          int
}

type groupKey struct {
	agentID   string
	runtimeID string
	tool      string
}

// Summarize groups Decision Ledger entries per agent, runtime, and tool.
func Summarize(entries []Entry) Report {
	report := Report{Total: len(entries)}
	groups := make(map[groupKey]*Group)
	for _, entry := range entries {
		tool := entry.CanonicalCapabilityID
		if tool == "" {
			tool = entry.ObservedToolName
		}
		key := groupKey{agentID: entry.AgentID, runtimeID: entry.RuntimeID, tool: tool}
		group := groups[key]
		if group == nil {
			group = &Group{
				AgentID:        entry.AgentID,
				RuntimeID:      entry.RuntimeID,
				Tool:           tool,
				PolicyDecision: entry.PolicyDecision,
			}
			groups[key] = group
		} else if group.PolicyDecision != entry.PolicyDecision {
			group.PolicyDecision = ""
		}
		group.Total++
		if entry.Differs {
			group.Diffs++
			report.Diffs++
		}
	}

	report.Groups = make([]Group, 0, len(groups))
	for _, group := range groups {
		report.Groups = append(report.Groups, *group)
	}
	sort.Slice(report.Groups, func(i, j int) bool {
		if report.Groups[i].AgentID != report.Groups[j].AgentID {
			return report.Groups[i].AgentID < report.Groups[j].AgentID
		}
		if report.Groups[i].RuntimeID != report.Groups[j].RuntimeID {
			return report.Groups[i].RuntimeID < report.Groups[j].RuntimeID
		}
		return report.Groups[i].Tool < report.Groups[j].Tool
	})
	return report
}
