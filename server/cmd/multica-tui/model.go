package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pennxiv/multica/server/cmd/multica-tui/api"
)

// ── Messages ─────────────────────────────────────────────────

type tickMsg time.Time
type dataLoadedMsg struct {
	agents     []api.Agent
	issues     []api.Issue
	runtimes   []api.Runtime
	dashboard  []api.DashboardAgentRunTime
	agentTasks map[string][]api.Task
	err        error
}
type errMsg struct{ error }

// ── Focus ─────────────────────────────────────────────────────

type Focus int

const (
	FocusIssues Focus = iota
	FocusCount
)

// ── Model ─────────────────────────────────────────────────────

type Model struct {
	client  *api.Client
	help    help.Model
	spinner spinner.Model

	focus      Focus
	loading    bool
	fetching   bool // guard against overlapping fetches
	err        error
	cursor  int

	agents    []api.Agent
	issues    []api.Issue
	runtimes  []api.Runtime
	dashboard []api.DashboardAgentRunTime
	agentTasks map[string][]api.Task

	keys keyMap

	lastUpdated time.Time
	width       int
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Refresh key.Binding
	Quit    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Quit, k.Refresh, k.Up, k.Down}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down},
		{k.Refresh},
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
				Token       string `json:"token"`
				AuthToken   string `json:"auth_token"`
				ServerURL   string `json:"server_url"`
				WorkspaceID string `json:"workspace_id"`
			}
			if json.Unmarshal(data, &cfg) == nil {
				if token == "" { token = cfg.Token }
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
			Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
			Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
			Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
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
		dash, _ := c.GetDashboardAgentRunTime()

		// Fetch tasks for all agents (for issue detail view)
		agentTasks := make(map[string][]api.Task)
		for _, a := range agents {
			tasks, err := c.GetAgentTasks(a.ID)
			if err == nil && len(tasks) > 0 {
				agentTasks[a.ID] = tasks
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
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ── Update ───────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.help.Width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Refresh):
			m.loading = true
			m.err = nil
			return m, tea.Batch(fetchData(m.client), m.spinner.Tick)
		case key.Matches(msg, m.keys.Down):
			m.cursor++
			m.clampCursor()
		case key.Matches(msg, m.keys.Up):
			m.cursor--
			m.clampCursor()
		}

	case errMsg:
		m.loading = false
		m.fetching = false
		m.err = msg.error
		return m, tick()

	case dataLoadedMsg:
		m.loading = false
		m.fetching = false
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
		if m.fetching {
			return m, tick() // skip this tick, reschedule
		}
		m.loading = true
		m.fetching = true
		return m, tea.Batch(fetchData(m.client), tick())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) clampCursor() {
	limit := max(0, len(m.issues)-1)
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > limit {
		m.cursor = limit
	}
}
