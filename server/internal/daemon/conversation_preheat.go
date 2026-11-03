package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/multica-ai/multica/server/internal/daemon/localhost"
)

const conversationPreheatFile = "conversation-preheat.json"

// conversationPreheat is this device's choice whether opening an existing
// conversation may start its process ahead of a message, which runs the project's
// SessionStart hooks. It covers conversations opened from other devices too, so
// it is only served on the loopback entry: another device cannot change it.
type conversationPreheat struct {
	path  string
	token string

	mu       sync.Mutex
	disabled bool
}

type conversationPreheatFileBody struct {
	OldConversations bool `json:"old_conversations"`
}

func newConversationPreheat(profileDir, token string) *conversationPreheat {
	p := &conversationPreheat{path: filepath.Join(profileDir, conversationPreheatFile), token: token}
	if data, err := os.ReadFile(p.path); err == nil {
		var saved conversationPreheatFileBody
		if json.Unmarshal(data, &saved) == nil {
			p.disabled = !saved.OldConversations
		}
	}
	return p
}

// enabled reports whether old conversations may preheat.
func (p *conversationPreheat) enabled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.disabled
}

func (p *conversationPreheat) set(enabled bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, err := json.Marshal(conversationPreheatFileBody{OldConversations: enabled})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.path), ".conversation-preheat-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), p.path); err != nil {
		return err
	}
	p.disabled = !enabled
	return nil
}

func (p *conversationPreheat) register(mux *http.ServeMux) {
	mux.HandleFunc("/local/conversation-preheat", p.handle)
}

func (p *conversationPreheat) handle(w http.ResponseWriter, r *http.Request) {
	if !localhost.AuthorizeDesktop(r, p.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var body struct {
			OldConversations *bool `json:"old_conversations"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil || body.OldConversations == nil {
			deviceLinkError(w, http.StatusBadRequest, "invalid_request", "old_conversations is required")
			return
		}
		if err := p.set(*body.OldConversations); err != nil {
			deviceLinkError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(conversationPreheatFileBody{OldConversations: p.enabled()})
}
