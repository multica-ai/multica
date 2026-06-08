package main

import "github.com/charmbracelet/lipgloss"

// ── Colors ───────────────────────────────────────────────────

var (
	clrGreen   = lipgloss.Color("#00ff00")
	clrRed     = lipgloss.Color("#ff0000")
	clrYellow  = lipgloss.Color("#ffff00")
	clrCyan    = lipgloss.Color("#00ffff")
	clrWhite   = lipgloss.Color("#ffffff")
	clrDim     = lipgloss.Color("#888888")
	clrBg      = lipgloss.Color("#1a1a2e")
	clrBgLight = lipgloss.Color("#16213e")
	clrBorder  = lipgloss.Color("#0f3460")
	clrAccent  = lipgloss.Color("#e94560")
	clrOrange  = lipgloss.Color("#ffa500")
	clrBlue    = lipgloss.Color("#00bfff")
)

// ── Styles ───────────────────────────────────────────────────

var (
	AppStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Background(clrBg).
			Width(100)

	TitleStyle = lipgloss.NewStyle().
			Foreground(clrCyan).
			Bold(true).
			Padding(0, 1)

	SectionStyle = lipgloss.NewStyle().
			Foreground(clrWhite).
			Bold(true).
			Padding(0, 1).
			MarginTop(1)

	StatusOnline  = lipgloss.NewStyle().Foreground(clrGreen).SetString("● ON")
	StatusOffline = lipgloss.NewStyle().Foreground(clrRed).SetString("○ OFF")
	StatusIdle    = lipgloss.NewStyle().Foreground(clrGreen).SetString("● idle")
	StatusBusy    = lipgloss.NewStyle().Foreground(clrYellow).SetString("● busy")
	StatusPaused  = lipgloss.NewStyle().Foreground(clrDim).SetString("◐ paused")
	StatusQueued  = lipgloss.NewStyle().Foreground(clrOrange).SetString("◉ queued")

	StatusTodo     = lipgloss.NewStyle().Foreground(clrDim).SetString("○ todo")
	StatusInProg   = lipgloss.NewStyle().Foreground(clrYellow).SetString("● in_progress")
	StatusDone     = lipgloss.NewStyle().Foreground(clrGreen).SetString("✓ done")
	StatusCancelled = lipgloss.NewStyle().Foreground(clrRed).SetString("✗ cancelled")

	RowStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(clrWhite)

	SelectedRowStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Foreground(clrCyan).
				Background(clrBgLight)

	DimStyle = lipgloss.NewStyle().
			Foreground(clrDim)

	HelpStyle = lipgloss.NewStyle().
			Foreground(clrDim).
			PaddingTop(1)

	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrBorder).
			Padding(0, 1).
			Width(48)

	AgentBox = BoxStyle.Copy().Width(48)
	IssueBox = BoxStyle.Copy().Width(56)
)

// ── Status helpers ───────────────────────────────────────────

func agentStatusStyle(status string) string {
	switch status {
	case "idle":
		return StatusIdle.String()
	case "busy", "working":
		return StatusBusy.String()
	case "paused":
		return StatusPaused.String()
	default:
		return lipgloss.NewStyle().Foreground(clrDim).SetString(status).String()
	}
}

func issueStatusStyle(status string) string {
	switch status {
	case "todo", "backlog":
		return StatusTodo.String()
	case "in_progress":
		return StatusInProg.String()
	case "done":
		return StatusDone.String()
	case "cancelled", "canceled":
		return StatusCancelled.String()
	default:
		return DimStyle.Render(status)
	}
}

func runtimeStatusStyle(status string) string {
	if status == "online" {
		return StatusOnline.String()
	}
	return StatusOffline.String()
}
