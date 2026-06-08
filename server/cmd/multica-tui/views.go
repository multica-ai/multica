package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/pennxiv/multica/server/cmd/multica-tui/api"
)

// ── Main View ────────────────────────────────────────────────

func (m Model) View() string {
	if m.loading && m.agents == nil {
		return m.viewLoading()
	}

	var b strings.Builder

	// Title
	title := TitleStyle.Render(" 🤖 Multica Dashboard")
	ts := DimStyle.Render(fmt.Sprintf("  last: %s", m.lastUpdated.Format("15:04:05")))
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Center, title, ts))
	b.WriteString("\n\n")

	// Error banner
	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().
			Foreground(clrAccent).
			Bold(true).
			Render("⚠ " + m.err.Error() + "\n\n"))
	}

	// Tabs
	b.WriteString(m.viewTabs())
	b.WriteString("\n")

	// Tab content
	switch m.tab {
	case TabAgents:
		b.WriteString(m.viewAgentOverview())
	case TabIssues:
		b.WriteString(m.viewIssueQueue())
	}

	// Help
	b.WriteString("\n")
	b.WriteString(HelpStyle.Render(m.help.View(m.keys)))

	return AppStyle.Render(b.String())
}

func (m Model) viewLoading() string {
	return AppStyle.Render(
		lipgloss.JoinVertical(lipgloss.Center,
			"",
			"",
			m.spinner.View()+" Loading...",
		),
	)
}

func (m Model) viewTabs() string {
	var tabs []string
	for i := 0; i < int(TabCount); i++ {
		t := Tab(i)
		style := lipgloss.NewStyle().
			Padding(0, 2).
			Bold(true).
			Foreground(clrDim).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(clrBorder)
		if i == int(m.tab) {
			style = style.Foreground(clrCyan).
				BorderForeground(clrCyan).
				Background(clrBgLight)
		}
		tabs = append(tabs, style.Render(t.String()))
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, tabs...)
}

// ── Agent Overview ───────────────────────────────────────────

func (m Model) viewAgentOverview() string {
	if len(m.agents) == 0 {
		return DimStyle.Render("  No agents found.")
	}

	// Build runtime lookup
	runtimeMap := make(map[string]string)
	for _, r := range m.runtimes {
		runtimeMap[r.ID] = r.Status
	}

	// Build dashboard runtime lookup
	dashMap := make(map[string]float64)
	for _, d := range m.dashboard {
		dashMap[d.AgentID] = d.TotalSeconds
	}

	var lines []string
	lines = append(lines, SectionStyle.Render(fmt.Sprintf("🧑‍💻 Agents (%d)", len(m.agents))))

	// Header
	header := lipgloss.NewStyle().Bold(true).Foreground(clrDim).
		Render(fmt.Sprintf("%-4s %-18s %-10s %-8s %-22s %s", "", "Name", "Status", "Runtime", "Model", "Tasks"))
	lines = append(lines, "  "+header)
	lines = append(lines, "")

	for i, a := range m.agents {
		icon := agentIcon(a.Name)
		runtimeStatus := runtimeMap[a.RuntimeID]
		runStr := runtimeStatusStyle(runtimeStatus)
		statStr := agentStatusStyle(a.Status)
		modelStr := shortModel(a.Model)
		taskCount := ""
		if tasks, ok := m.agentTasks[a.ID]; ok && len(tasks) > 0 {
			// Show latest task status
			running := 0
			for _, t := range tasks {
				if t.Status == "in_progress" || t.Status == "running" {
					running++
				}
			}
			if running > 0 {
				taskCount = lipgloss.NewStyle().Foreground(clrYellow).Render(fmt.Sprintf("🏃%d", running))
			} else {
				taskCount = DimStyle.Render(fmt.Sprintf("%d tasks", len(tasks)))
			}
		}

		line := fmt.Sprintf("%-4s %-18s %-10s %-8s %-22s %s", icon, a.Name, statStr, runStr, modelStr, taskCount)

		if i == m.cursor {
			lines = append(lines, "  "+SelectedRowStyle.Render(line))
		} else {
			lines = append(lines, "  "+RowStyle.Render(line))
		}
	}

	// Runtime summary
	lines = append(lines, "")
	lines = append(lines, SectionStyle.Render(fmt.Sprintf("⚡ Runtimes (%d)", len(m.runtimes))))
	for _, r := range m.runtimes {
		rs := runtimeStatusStyle(r.Status)
		lines = append(lines, fmt.Sprintf("    %s %s", rs, r.Name))
	}

	return strings.Join(lines, "\n")
}

// ── Issue Queue ──────────────────────────────────────────────

func (m Model) viewIssueQueue() string {
	// Sort: in_progress first, then todo, then done, rest
	sorted := make([]api.Issue, len(m.issues))
	copy(sorted, m.issues)
	sort.SliceStable(sorted, func(i, j int) bool {
		return issuePriority(sorted[i].Status) < issuePriority(sorted[j].Status)
	})

	if len(sorted) == 0 {
		return DimStyle.Render("  No issues.")
	}

	var lines []string
	lines = append(lines, SectionStyle.Render(fmt.Sprintf("📋 Issues (%d)", len(sorted))))

	// Header
	header := lipgloss.NewStyle().Bold(true).Foreground(clrDim).
		Render(fmt.Sprintf("%-8s %-50s %-14s %-8s %s", "ID", "Title", "Status", "Pri", "Assignee"))
	lines = append(lines, "  "+header)
	lines = append(lines, "")

	for i, issue := range sorted {
		cursor := ""
		if i == m.cursor {
			cursor = "▸"
		}

		statusStr := issueStatusStyle(issue.Status)
		priStr := priorityColor(issue.Priority)
		assignee := m.agentName(issue.AssigneeID)

		id := issue.Identifier
		if id == "" {
			id = fmt.Sprintf("#%d", issue.Number)
		}
		title := issue.Title
		if len([]rune(title)) > 47 {
			title = string([]rune(title)[:44]) + "..."
		}

		line := fmt.Sprintf("%s %-8s %-50s %-14s %-8s %s", cursor, id, title, statusStr, priStr, assignee)

		if i == m.cursor {
			lines = append(lines, "  "+SelectedRowStyle.Render(line))
		} else {
			lines = append(lines, "  "+RowStyle.Render(line))
		}

		// Show task detail on selected
		if i == m.cursor {
			// Check if there are active tasks for this issue
			for _, tasks := range m.agentTasks {
				for _, t := range tasks {
					if t.IssueID == issue.ID && (t.Status == "in_progress" || t.Status == "running" || t.Status == "queued") {
						detail := fmt.Sprintf("      %s %s [%s] attempt=%d",
							taskKindIcon(t.Kind), DimStyle.Render(t.ID[:12]),
							issueStatusStyle(t.Status), t.Attempt)
						if t.StartedAt != nil {
							start, _ := time.Parse(time.RFC3339, *t.StartedAt)
							detail += DimStyle.Render(fmt.Sprintf(" %s ago", time.Since(start).Round(time.Second)))
						}
						lines = append(lines, "  "+detail)
					}
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}

// ── Helpers ──────────────────────────────────────────────────

func (m Model) agentName(assigneeID string) string {
	for _, a := range m.agents {
		if a.ID == assigneeID {
			return a.Name
		}
	}
	if len(assigneeID) > 8 {
		return assigneeID[:8]
	}
	return assigneeID
}

func issuePriority(status string) int {
	switch status {
	case "in_progress":
		return 0
	case "todo", "backlog":
		return 1
	case "done":
		return 3
	case "cancelled", "canceled":
		return 4
	default:
		return 2
	}
}
