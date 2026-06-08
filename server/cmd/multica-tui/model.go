package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pennxiv/multica/server/cmd/multica-tui/api"
)

// ── Messages ─────────────────────────────────────────────────

type tickMsg time.Time
type dataLoadedMsg struct {
	agents         []api.Agent
	issues         []api.Issue
	runtimes       []api.Runtime
	dashboard      []api.DashboardAgentRunTime
	agentTasks     map[string][]api.Task
	err            error
}

type errMsg struct{ error }

// ── Tabs ─────────────────────────────────────────────────────

type Tab int

const (
	TabAgents Tab = iota
	TabIssues
	TabCount
)

func (t Tab) String() string {
	switch t {
	case TabAgents:
		return " Agents "
	case TabIssues:
		return " Issues "
	default:
		return "?"
	}
}

// ── Model ────────────────────────────────────────────────────

type Model struct {
	client  *api.Client
	help    help.Model
	spinner spinner.Model

	tab      Tab
	loading  bool
	err      error
	cursor   int

	agents    []api.Agent
	issues    []api.Issue
	runtimes  []api.Runtime
	dashboard []api.DashboardAgentRunTime
	agentTasks map[string][]api.Task

	// Key bindings
	keys keyMap

	// last successful fetch time
	lastUpdated time.Time
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Left    key.Binding
	Right   key.Binding
	Enter   key.Binding
	Refresh key.Binding
	Quit    key.Binding
	TabNext key.Binding
	TabPrev key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Refresh, k.TabNext, k.Up, k.Down}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},
		{k.Enter, k.Refresh, k.TabNext, k.TabPrev},
		{k.Quit},
	}
}

func NewModel() *Model {
	token := os.Getenv("MULTICA_TOKEN")
	baseURL := os.Getenv("MULTICA_API_URL")
	wsID := os.Getenv("MULTICA_WORKSPACE_ID")
	if baseURL == "" {
		baseURL = "http://172.20.3.43:8080"
	}
	if wsID == "" {
		wsID = "25c422ca-babf-4b73-bd38-872007769138"
	}
	// Fallback: read from multica config
	if token == "" {
		if data, err := os.ReadFile(os.ExpandEnv("$HOME/.multica/config.json")); err == nil {
			var cfg struct {
				AuthToken   string `json:"auth_token"`
				ServerURL   string `json:"server_url"`
				WorkspaceID string `json:"workspace_id"`
			}
			if json.Unmarshal(data, &cfg) == nil {
				if token == "" { token = cfg.AuthToken }
				if baseURL == "" { baseURL = cfg.ServerURL }
				if wsID == "" { wsID = cfg.WorkspaceID }
			}
		}
	}

	s := spinner.New()
	s.Style = lipgloss.NewStyle().Foreground(clrCyan)
	s.Spinner = spinner.Dot

	return &Model{
		client:  api.New(baseURL, token, wsID),
		help:    help.New(),
		spinner: s,
		loading: true,
		keys: keyMap{
			Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
			Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
			Left:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "prev tab")),
			Right:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next tab")),
			Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
			TabNext: key.NewBinding(key.WithKeys("tab", "l"), key.WithHelp("tab", "next tab")),
			TabPrev: key.NewBinding(key.WithKeys("shift+tab", "h"), key.WithHelp("S-tab", "prev tab")),
		},
	}
}

// ── Init ─────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, fetchData(m.client), tick())
}

// ── Fetch ────────────────────────────────────────────────────

func fetchData(c *api.Client) tea.Cmd {
	return func() tea.Msg {
		agents, err := c.ListAgents()
		if err != nil {
			return errMsg{err}
		}
		issues, err := c.ListIssues()
		if err != nil {
			return errMsg{err}
		}
		runtimes, err := c.ListAgentRuntimes()
		if err != nil {
			return errMsg{err}
		}
		dash, err := c.GetDashboardAgentRunTime()
		if err != nil {
			// non-fatal
		}

		// Fetch tasks for busy agents
		agentTasks := make(map[string][]api.Task)
		for _, a := range agents {
			if a.Status == "busy" || a.Status == "working" {
				tasks, err := c.GetAgentTasks(a.ID)
				if err == nil && len(tasks) > 0 {
					agentTasks[a.ID] = tasks
				}
			}
		}

		return dataLoadedMsg{
			agents:     agents,
			issues:     issues,
			runtimes:   runtimes,
			dashboard:  dash,
			agentTasks: agentTasks,
		}
	}
}

func tick() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ── Update ───────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.help.Width = msg.Width
		AppStyle = AppStyle.Width(msg.Width - 4)
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Refresh):
			m.loading = true
			m.err = nil
			return m, tea.Batch(fetchData(m.client), m.spinner.Tick)
		case key.Matches(msg, m.keys.TabNext):
			m.tab = (m.tab + 1) % TabCount
			m.cursor = 0
		case key.Matches(msg, m.keys.TabPrev):
			m.tab = (m.tab - 1 + TabCount) % TabCount
			m.cursor = 0
		case key.Matches(msg, m.keys.Up):
			m.cursor--
			m.clampCursor()
		case key.Matches(msg, m.keys.Down):
			m.cursor++
			m.clampCursor()
		case key.Matches(msg, m.keys.Left):
			m.tab = (m.tab - 1 + TabCount) % TabCount
			m.cursor = 0
		case key.Matches(msg, m.keys.Right):
			m.tab = (m.tab + 1) % TabCount
			m.cursor = 0
		}

	case errMsg:
		m.loading = false
		m.err = msg.error
		return m, tick()

	case dataLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.agents = msg.agents
			m.issues = msg.issues
			m.runtimes = msg.runtimes
			m.dashboard = msg.dashboard
			m.agentTasks = msg.agentTasks
			m.lastUpdated = time.Now()
		}
		return m, tick()

	case tickMsg:
		m.loading = true
		return m, fetchData(m.client)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) clampCursor() {
	limit := 0
	switch m.tab {
	case TabAgents:
		limit = max(0, len(m.agents)-1)
	case TabIssues:
		limit = max(0, len(m.issues)-1)
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > limit {
		m.cursor = limit
	}
}

// ── Rendering helpers ────────────────────────────────────────

func agentIcon(name string) string {
	switch name {
	case "Rana":     return "🐸"
	case "Tom":      return "🐱"
	case "crayon":   return "✏️"
	case "Mr. Chicken": return "🐔"
	default: return "🤖"
	}
}

func priorityColor(p string) string {
	switch p {
	case "urgent", "critical":  return lipgloss.NewStyle().Foreground(clrAccent).Render("⬆ " + p)
	case "high":               return lipgloss.NewStyle().Foreground(clrOrange).Render("↑ " + p)
	case "medium":             return lipgloss.NewStyle().Foreground(clrYellow).Render("→ " + p)
	case "low":                return lipgloss.NewStyle().Foreground(clrDim).Render("↓ " + p)
	default:                   return lipgloss.NewStyle().Foreground(clrDim).Render(p)
	}
}

func shortModel(s string) string {
	if len(s) > 24 {
		return s[:22] + ".."
	}
	return s
}

func taskKindIcon(kind string) string {
	switch kind {
	case "code", "implement": return "💻"
	case "review":            return "👀"
	case "debug", "fix":      return "🔧"
	case "research":          return "🔍"
	case "test":              return "🧪"
	default:                  return "⚡"
	}
}
