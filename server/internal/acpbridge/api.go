// Package acpbridge exposes existing Multica issue workers to ACP clients.
// It never starts a provider process or reads provider session files.
package acpbridge

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/interaction"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type API interface {
	GetJSON(context.Context, string, any) error
	PostJSON(context.Context, string, any, any) error
}

type run struct {
	ID        string `json:"id"`
	IssueID   string `json:"issue_id"`
	AgentID   string `json:"agent_id"`
	Status    string `json:"status"`
	WorkDir   string `json:"work_dir"`
	CreatedAt string `json:"created_at"`
}

func (r run) terminal() bool {
	return r.Status == "completed" || r.Status == "failed" || r.Status == "cancelled"
}

type conversation struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func conversationID(workspace, issue, agent string) string {
	return workspace + "/" + issue + "/" + agent
}

func splitID(id, workspace string) (string, string, error) {
	p := strings.Split(id, "/")
	if len(p) != 3 || p[0] != workspace {
		return "", "", errors.New("conversation belongs to another workspace or has an invalid ID")
	}
	for _, v := range p {
		if _, err := uuid.Parse(v); err != nil {
			return "", "", errors.New("invalid conversation ID")
		}
	}
	return p[1], p[2], nil
}

func (b *Bridge) runs(ctx context.Context, id string) ([]run, error) {
	issue, agent, err := splitID(id, b.workspace)
	if err != nil {
		return nil, err
	}
	// The agent endpoint owns private-agent visibility; check on every refresh,
	// not just discovery, so revoked access cannot keep a subscription alive.
	var visible struct {
		ID string `json:"id"`
	}
	if err = b.api.GetJSON(ctx, "/api/agents/"+agent, &visible); err != nil {
		return nil, err
	}
	var all []run
	if err = b.api.GetJSON(ctx, "/api/issues/"+issue+"/task-runs", &all); err != nil {
		return nil, err
	}
	out := make([]run, 0, len(all))
	for _, r := range all {
		if r.AgentID == agent && r.IssueID == issue {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	if len(out) == 0 {
		return nil, errors.New("issue conversation has no runs")
	}
	return out, nil
}

func currentRun(runs []run) (run, error) {
	selected := runs[len(runs)-1]
	active := 0
	for _, r := range runs {
		if !r.terminal() {
			active++
			selected = r
		}
	}
	if active > 1 {
		return run{}, errors.New("multiple active runs: use Multica to select or resolve them")
	}
	return selected, nil
}

func (b *Bridge) record(ctx context.Context, r run) (*interaction.Record, error) {
	var out *interaction.Record
	err := b.api.GetJSON(ctx, "/api/tasks/"+url.PathEscape(r.ID)+"/interaction", &out)
	return out, err
}

func (b *Bridge) messages(ctx context.Context, r run, since int) ([]protocol.TaskMessagePayload, error) {
	var out []protocol.TaskMessagePayload
	err := b.api.GetJSON(ctx, fmt.Sprintf("/api/tasks/%s/messages?since=%d", url.PathEscape(r.ID), since), &out)
	return out, err
}

func (b *Bridge) discover(ctx context.Context, cwd string) ([]conversation, error) {
	var agents []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := b.api.GetJSON(ctx, "/api/agents", &agents); err != nil {
		return nil, err
	}
	out := []conversation{}
	for _, a := range agents {
		var runs []run
		if err := b.api.GetJSON(ctx, "/api/agents/"+url.PathEscape(a.ID)+"/tasks", &runs); err != nil {
			var he *cli.HTTPError
			if errors.As(err, &he) && (he.StatusCode == 403 || he.StatusCode == 404) {
				continue
			}
			return nil, err
		}
		groups := map[string][]run{}
		for _, r := range runs {
			if r.IssueID != "" {
				groups[r.IssueID] = append(groups[r.IssueID], r)
			}
		}
		for issue, group := range groups {
			sort.Slice(group, func(i, j int) bool { return group[i].CreatedAt < group[j].CreatedAt })
			r, err := currentRun(group)
			if err != nil {
				r = group[len(group)-1]
			}
			var info struct {
				Title      string `json:"title"`
				Identifier string `json:"identifier"`
			}
			if err := b.api.GetJSON(ctx, "/api/issues/"+url.PathEscape(issue), &info); err != nil {
				continue
			}
			label := info.Identifier
			if label == "" {
				label = issue
			}
			location := r.WorkDir
			if location == "" {
				location = cwd
			}
			out = append(out, conversation{conversationID(b.workspace, issue, a.ID), fmt.Sprintf("%s · %s · %s · %s", label, info.Title, a.Name, r.Status), location, r.CreatedAt})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].SessionID < out[j].SessionID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}
