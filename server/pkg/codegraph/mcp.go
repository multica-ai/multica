package codegraph

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Repository struct {
	Sources       []string `json:"sources"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Branch        string   `json:"branch"`
	Status        string   `json:"status"`
	Commit        string   `json:"commit"`
	ActiveBuildID string   `json:"active_build_id,omitempty"`
}

// Store must resolve current grants for every call, including file access.
type Store interface {
	List(context.Context) ([]Repository, error)
	Checkout(context.Context, string) (string, error)
}

type searchInput struct {
	RepositoryID string `json:"repository_id" jsonschema:"ID from codegraph_list_repositories"`
	Query        string `json:"query" jsonschema:"Literal text to find (case-sensitive)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum matches, 1 to 50, default 20"`
}

type readInput struct {
	RepositoryID string `json:"repository_id"`
	BuildID      string `json:"build_id,omitempty" jsonschema:"Successful build ID from graph info"`
	Path         string `json:"path" jsonschema:"Relative file path"`
	Start        int    `json:"start_line,omitempty" jsonschema:"First line (1-based), default 1"`
	End          int    `json:"end_line,omitempty" jsonschema:"Last line (inclusive), at most 200 lines"`
}

func NewServer(store Store, modules ...func(*mcp.Server)) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "multica-codegraph", Version: "1.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "codegraph_list_repositories", Description: "List CodeGraph repositories currently assigned to this agent. Assignments are refreshed on every call."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			repos, err := store.List(ctx)
			if repos == nil {
				repos = []Repository{}
			}
			return nil, map[string]any{"repositories": repos}, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "codegraph_search", Description: "Search literal text in an assigned repository checkout. Returns paths and line numbers. Results are capped by limit; binary and large files are skipped. This is text search, not a call-graph query. For code/document structure and commit-pinned evidence use codegraph_query."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input searchInput) (*mcp.CallToolResult, any, error) {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			dir, err := store.Checkout(ctx, input.RepositoryID)
			if err != nil {
				return nil, nil, err
			}
			if input.Limit == 0 {
				input.Limit = 20
			}
			matches, err := Search(ctx, dir, input.Query, input.Limit)
			return nil, map[string]any{"matches": matches, "limit": input.Limit}, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "codegraph_read_file", Description: "Read up to 200 numbered lines of a text file in an assigned, ready CodeGraph repository. This reads the current checkout; use codegraph_query operation=read with build_id for a fixed source version."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input readInput) (*mcp.CallToolResult, any, error) {
			if input.BuildID != "" {
				return nil, nil, fmt.Errorf("Pinned reads require the graph query module")
			}
			dir, err := store.Checkout(ctx, input.RepositoryID)
			if err != nil {
				return nil, nil, err
			}
			if input.Start == 0 {
				input.Start = 1
			}
			if input.End == 0 {
				input.End = input.Start + 199
			}
			lines, err := ReadFile(dir, input.Path, input.Start, input.End)
			return nil, map[string]any{"lines": lines}, err
		})
	for _, register := range modules {
		register(server)
	}
	return server
}
