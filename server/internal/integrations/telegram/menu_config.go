package telegram

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// MenuConfig is an optional, installation-scoped menu. An empty private-chat
// allowlist disables it. Requests are ordinary text sent through the router,
// not tool calls or a substitute for the agent's confirmation policy.
type MenuConfig struct {
	PrivateChatIDs []int64    `json:"private_chat_ids"`
	RootText       string     `json:"root_text"`
	BackText       string     `json:"back_text"`
	Pages          []MenuPage `json:"pages"`
}

type MenuPage struct {
	Command string       `json:"command"`
	Title   string       `json:"title"`
	Actions []MenuAction `json:"actions"`
}

type MenuAction struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Request string `json:"request"`
}

var menuIDPattern = regexp.MustCompile(`^[a-z0-9_]{1,24}$`)

var ErrInvalidMenuConfig = errors.New("telegram: invalid menu config")

// ValidateMenuConfig bounds keyboards and callback payloads before storage or
// channel construction. Nil preserves the existing, menu-free installation.
func ValidateMenuConfig(m *MenuConfig) error {
	if m == nil {
		return nil
	}
	if len(m.PrivateChatIDs) > 100 || len(m.Pages) == 0 || len(m.Pages) > 32 {
		return fmt.Errorf("%w: menu requires 1–32 pages and at most 100 private chats", ErrInvalidMenuConfig)
	}
	for _, id := range m.PrivateChatIDs {
		if id <= 0 {
			return fmt.Errorf("%w: menu chat IDs must be positive private-chat IDs", ErrInvalidMenuConfig)
		}
	}
	if strings.TrimSpace(m.RootText) == "" || len(m.RootText) > 512 || strings.TrimSpace(m.BackText) == "" || len(m.BackText) > 64 {
		return fmt.Errorf("%w: menu root_text and back_text are required (maximum 512/64 bytes)", ErrInvalidMenuConfig)
	}
	pages := make(map[string]bool)
	for _, p := range m.Pages {
		if !menuIDPattern.MatchString(p.Command) || p.Command == "root" || p.Command == "new" || p.Command == "clear" || p.Command == "issue" || pages[p.Command] || len(p.Actions) == 0 || len(p.Actions) > 32 || strings.TrimSpace(p.Title) == "" || len(p.Title) > 128 {
			return fmt.Errorf("%w: invalid or duplicate menu page", ErrInvalidMenuConfig)
		}
		pages[p.Command] = true
		actions := make(map[string]bool)
		for _, a := range p.Actions {
			if !menuIDPattern.MatchString(a.ID) || actions[a.ID] || strings.TrimSpace(a.Label) == "" || len(a.Label) > 64 || strings.TrimSpace(a.Request) == "" || len(a.Request) > 4096 {
				return fmt.Errorf("%w: invalid or duplicate menu action", ErrInvalidMenuConfig)
			}
			actions[a.ID] = true
		}
	}
	return nil
}

func (m *MenuConfig) page(id string) (MenuPage, bool) {
	for _, p := range m.Pages {
		if p.Command == id {
			return p, true
		}
	}
	return MenuPage{}, false
}
