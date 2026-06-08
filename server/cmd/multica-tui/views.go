package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/pennxiv/multica/server/cmd/multica-tui/api"
)

const multicaArt = ` __  __      _ _   _         
|  \/  |_  _| | |_(_)__ __ _ 
| |\/| | || | |  _| / _/ _` + "`" + ` |
|_|  |_|\_,_|_|\__|_\__\__,_|`

func (m Model) View() string {
	width := m.width
	if width < 2 {
		width = 100
	}
	if m.loading && m.agents == nil {
		return AppStyle.Render(
			lipgloss.JoinVertical(lipgloss.Center, "", "",
				m.spinner.View()+" Loading..."),
		)
	}

	innerW := width - 4
	var sec []string

	// ── Header ──
	{
		artStyle := lipgloss.NewStyle().Foreground(clrCyan)
		artLines := strings.Split(multicaArt, "\n")
		innerW := width - 4

		// Stats line beside art (aligned to rightmost art line)
		online := 0
		for _, a := range m.agents {
			if a.Status == "idle" || a.Status == "busy" || a.Status == "working" {
				online++
			}
		}
		stats := fmt.Sprintf(" %d agents · %d issues  %s ", online, len(m.issues), m.lastUpdated.Format("15:04:05"))

		// Build header: art lines with stats on the last line
		for i, line := range artLines {
			if i == len(artLines)-1 {
				pad := innerW - lipgloss.Width(artStyle.Render(line)) - len(stats)
				if pad < 1 {
					pad = 1
				}
				sec = append(sec, artStyle.Render(line)+strings.Repeat(" ", pad)+DimStyle.Render(stats))
			} else {
				sec = append(sec, artStyle.Render(line))
			}
		}
	}

	if m.err != nil {
		sec = append(sec, lipgloss.NewStyle().Foreground(clrAccent).Bold(true).Render("⚠ "+m.err.Error()))
	}

	sec = append(sec, m.renderAgents(innerW))
	sec = append(sec, m.renderIssues(innerW))

	taskS := m.renderTaskDetail(innerW)
	if taskS != "" {
		sec = append(sec, taskS)
	}

	activeS := m.renderActive(innerW)
	if activeS != "" {
		sec = append(sec, activeS)
	}

	sec = append(sec, HelpStyle.Render(m.help.View(m.keys)))
	return AppStyle.Width(width-2).Render(lipgloss.JoinVertical(lipgloss.Left, sec...))
}

// ── Agents ───────────────────────────────────────────────────

func (m Model) renderAgents(w int) string {
	runtimeMap := make(map[string]string)
	for _, r := range m.runtimes {
		runtimeMap[r.ID] = r.Status
	}

	var lines []string
	lines = append(lines, secTitle(" Agents ", w))

	for _, a := range m.agents {
		icon := agentIcon(a.Name)
		statStr := agentStatusStyle(a.Status)
		modelStr := shortModel(a.Model)
		runStr := runtimeStatusStyle(runtimeMap[a.RuntimeID])

		taskInfo := ""
		if tasks, ok := m.agentTasks[a.ID]; ok && len(tasks) > 0 {
			n := 0
			for _, t := range tasks {
				if t.Status == "in_progress" || t.Status == "running" {
					n++
				}
			}
			if n > 0 {
				taskInfo = lipgloss.NewStyle().Foreground(clrYellow).Render(fmt.Sprintf(" 🏃%d", n))
			} else {
				taskInfo = DimStyle.Render(fmt.Sprintf(" %dt", len(tasks)))
			}
		}

		lines = append(lines, fmt.Sprintf("  %s  %s%s %s%s",
				icon,
				lipgloss.NewStyle().Width(12).Render(a.Name),
				lipgloss.NewStyle().Width(12).Render(statStr),
				runStr, taskInfo))
		lines = append(lines, fmt.Sprintf("     %s", modelStr))
	}
	return strings.Join(lines, "\n")
}

// ── Issues ───────────────────────────────────────────────────

func (m Model) renderIssues(w int) string {
	sorted := make([]api.Issue, len(m.issues))
	copy(sorted, m.issues)
	sort.SliceStable(sorted, func(i, j int) bool {
		return issuePrio(sorted[i].Status) < issuePrio(sorted[j].Status)
	})

	var lines []string
	lines = append(lines, secTitle(fmt.Sprintf(" Issues (%d) ", len(sorted)), w))
	if len(sorted) == 0 {
		lines = append(lines, "  "+DimStyle.Render("No issues."))
		return strings.Join(lines, "\n")
	}

	// Fixed column widths (visible chars)
	const (
		wCursor   = 2
		wStatus   = 14
		wAgent    = 9
		wID       = 10
		wPriority = 10
	)
	wTitle := w - wCursor - wStatus - wAgent - wID - wPriority - 6
	if wTitle < 6 {
		wTitle = 6
	}

	for i, issue := range sorted {
		cursor := " "
		if i == m.cursor {
			cursor = "▸"
		}

		statusStr := issueStatusStyle(issue.Status)
		assignee := m.agentName(issue.AssigneeID)
		id := issue.Identifier
		if id == "" {
			id = fmt.Sprintf("#%d", issue.Number)
		}
		priStr := priorityColor(issue.Priority)

		// Title truncate by visible width
		titleTxt := issue.Title
		if lipgloss.Width(titleTxt) > wTitle {
			for lipgloss.Width(titleTxt) > wTitle-3 {
				titleTxt = string([]rune(titleTxt)[:len([]rune(titleTxt))-1])
			}
			titleTxt += "..."
		}

		// Build row with fixed-width cells using lipgloss
		parts := []string{
			cursor,
			lipgloss.NewStyle().Width(wStatus).Render(statusStr),
			lipgloss.NewStyle().Width(wAgent).Render(assignee),
			lipgloss.NewStyle().Width(wID).Render(id),
			titleTxt,
			lipgloss.NewStyle().Width(wPriority).Render(priStr),
		}
		row := strings.Join(parts, " ")

		if i == m.cursor {
			lines = append(lines, "  "+SelectedRowStyle.Render(row))
		} else {
			lines = append(lines, "  "+RowStyle.Render(row))
		}
	}
	return strings.Join(lines, "\n")
}

// ── Task Detail ──────────────────────────────────────────────

func (m Model) renderTaskDetail(w int) string {
	if len(m.issues) == 0 || m.cursor < 0 {
		return ""
	}
	sorted := make([]api.Issue, len(m.issues))
	copy(sorted, m.issues)
	sort.SliceStable(sorted, func(i, j int) bool {
		return issuePrio(sorted[i].Status) < issuePrio(sorted[j].Status)
	})
	if m.cursor >= len(sorted) {
		return ""
	}
	sel := sorted[m.cursor]

	var taskLines []string
	for _, tasks := range m.agentTasks {
		for _, t := range tasks {
			if t.IssueID != sel.ID {
				continue
			}
			statusStr := issueStatusStyle(t.Status)
			startStr := ""
			if t.StartedAt != nil {
				start, err := time.Parse(time.RFC3339, *t.StartedAt)
				if err == nil {
					startStr = fmt.Sprintf(" %s", time.Since(start).Round(time.Second))
				}
			}
			errorStr := ""
			if t.Error != nil && *t.Error != "" {
				errorStr = "  " + lipgloss.NewStyle().Foreground(clrAccent).Render("✗ "+*t.Error)
			}
			attemptStr := ""
			if t.Attempt > 1 {
				attemptStr = fmt.Sprintf(" attempt=%d", t.Attempt)
			}
			taskLines = append(taskLines,
				fmt.Sprintf("   %s  %s%s%s%s%s",
					taskKindIcon(t.Kind),
					statusStr, DimStyle.Render(startStr),
					DimStyle.Render(attemptStr),
					errorStr,
					DimStyle.Render("  "+t.ID[:min(12, len(t.ID))]),
				))
		}
	}

	if len(taskLines) == 0 {
		taskLines = append(taskLines, "   "+DimStyle.Render("No active tasks for this issue"))
	}

	lines := []string{secTitle(fmt.Sprintf(" %s: Tasks ", sel.Identifier), w)}
	lines = append(lines, taskLines...)
	return strings.Join(lines, "\n")
}

// ── Active ───────────────────────────────────────────────────

func (m Model) renderActive(w int) string {
	type at struct {
		name string
		task api.Task
	}
	var active []at
	for _, a := range m.agents {
		if tasks, ok := m.agentTasks[a.ID]; ok {
			for _, t := range tasks {
				if t.Status == "in_progress" || t.Status == "running" || t.Status == "queued" {
					active = append(active, at{name: a.Name, task: t})
				}
			}
		}
	}
	if len(active) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, secTitle(fmt.Sprintf(" Active (%d) ", len(active)), w))
	for _, a := range active {
		t := a.task
		statusStr := issueStatusStyle(t.Status)
		startStr := ""
		if t.StartedAt != nil {
			start, err := time.Parse(time.RFC3339, *t.StartedAt)
			if err == nil {
				startStr = fmt.Sprintf(" %s", time.Since(start).Round(time.Second))
			}
		}
		lines = append(lines,
			fmt.Sprintf("   %s  [%s]  %s%s",
				taskKindIcon(t.Kind), a.name, statusStr,
				DimStyle.Render(startStr),
			))
	}
	return strings.Join(lines, "\n")
}

// ── helpers ──────────────────────────────────────────────────

func secTitle(text string, w int) string {
	s := lipgloss.NewStyle().Bold(true).Foreground(clrWhite).Render(text)
	n := w - lipgloss.Width(s) - 1
	if n < 1 {
		n = 1
	}
	return s + lipgloss.NewStyle().Foreground(clrBorder).Render(strings.Repeat("─", n)) + "\n"
}

func (m Model) agentName(id string) string {
	for _, a := range m.agents {
		if a.ID == id {
			return a.Name
		}
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func issuePrio(s string) int {
	switch s {
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

func agentIcon(n string) string {
	switch n {
	case "Rana":
		return "🐸"
	case "Tom":
		return "🐱"
	case "crayon":
		return "✏️"
	case "Mr. Chicken":
		return "🐔"
	default:
		return "🤖"
	}
}

func taskKindIcon(k string) string {
	switch k {
	case "code", "implement":
		return "💻"
	case "review":
		return "👀"
	case "debug", "fix":
		return "🔧"
	case "research":
		return "🔍"
	case "test":
		return "🧪"
	default:
		return "⚡"
	}
}

func shortModel(s string) string {
	if len(s) > 24 {
		return s[:22] + ".."
	}
	return s
}
