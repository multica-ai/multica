package handler

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/codegraph"
)

// executeCodeGraphQuery requires the caller to authorize the repository before entry.
func (h *Handler) executeCodeGraphQuery(ctx context.Context, row codeGraphRepositoryRow, input codegraph.QueryInput) (codegraph.QueryResult, int, error) {
	var result codegraph.QueryResult
	if err := input.Normalize(); err != nil {
		return result, 400, err
	}
	build := row.ActiveBuildID
	if input.BuildID != "" {
		parsed, err := util.ParseUUID(input.BuildID)
		if err != nil {
			return result, 400, errors.New("Invalid build id")
		}
		build = parsed
	}
	if !build.Valid {
		return result, 409, errors.New("No successful graph build; synchronize first")
	}
	var commit string
	if err := h.DB.QueryRow(ctx, `SELECT commit_hash FROM code_graph_build WHERE id=$1 AND repository_id=$2 AND workspace_id=$3 AND status='ready'`, build, row.ID, row.WorkspaceID).Scan(&commit); err != nil {
		return result, 404, errors.New("Successful build unavailable")
	}
	dir := codeGraphBuildDir(uuidToString(row.WorkspaceID), uuidToString(row.ID), uuidToString(build))
	loadDocuments := func() (*codegraph.DocumentIndex, error) {
		return codegraph.LoadDocuments(filepath.Join(filepath.Dir(dir), "documents.json"), uuidToString(build), commit)
	}
	var data any
	truncated := false
	switch input.Operation {
	case "read":
		lines, err := codegraph.ReadFile(dir, input.Path, input.StartLine, input.EndLine)
		if err != nil {
			return result, 400, err
		}
		data = lines
	case "documents", "document_refs":
		index, err := loadDocuments()
		if err != nil {
			return result, 409, err
		}
		if input.Operation == "documents" {
			data = index.Search(input.Query)
			if input.Limit > 0 {
				items := data.(map[string]any)["nodes"].([]codegraph.DocumentNode)
				if len(items) > input.Limit {
					data.(map[string]any)["nodes"] = items[:input.Limit]
					data.(map[string]any)["truncated"] = true
				}
			}
		} else {
			data, err = index.References(input.SymbolID)
			if err != nil {
				return result, 404, err
			}
			if input.Limit > 0 {
				items := data.(map[string]any)["edges"].([]codegraph.DocumentEdge)
				if len(items) > input.Limit {
					data.(map[string]any)["edges"] = items[:input.Limit]
					data.(map[string]any)["truncated"] = true
				}
			}
		}
	default:
		queryCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		raw, err := h.codeGraphEngine(queryCtx, map[string]any{"op": input.Operation, "q": input.Query, "symbol_id": input.SymbolID, "limit": input.Limit, "workspace_id": uuidToString(row.WorkspaceID), "repository_id": uuidToString(row.ID), "build_id": uuidToString(build)})
		if err != nil {
			return result, 502, err
		}
		var envelope struct {
			Data      json.RawMessage `json:"data"`
			Truncated bool            `json:"truncated"`
		}
		if json.Unmarshal(raw, &envelope) != nil || len(envelope.Data) == 0 {
			return result, 502, errors.New("Invalid graph response")
		}
		data = envelope.Data
		truncated = envelope.Truncated
		var bounded struct {
			Truncated bool `json:"truncated"`
		}
		if json.Unmarshal(envelope.Data, &bounded) == nil {
			truncated = truncated || bounded.Truncated
		}
		if input.Operation == "info" {
			var info map[string]any
			if json.Unmarshal(envelope.Data, &info) != nil || info == nil {
				return result, 502, errors.New("Invalid graph response")
			}
			index, err := loadDocuments()
			if err != nil {
				info["documents"] = map[string]any{"available": false, "reason": err.Error()}
			} else {
				info["documents"] = map[string]any{"available": true, "coverage": index.Coverage, "nodes": len(index.Nodes), "edges": len(index.Edges)}
			}
			data = info
		}
	}
	if object, ok := data.(map[string]any); ok {
		if flag, ok := object["truncated"].(bool); ok {
			truncated = flag
		}
	}
	return codegraph.QueryResult{RepositoryID: uuidToString(row.ID), RepositoryName: row.RepoName, BuildID: uuidToString(build), Commit: commit, EngineCommit: "19813544766f74a10f403c9e474306c0f7b61a18", Operation: input.Operation, Truncated: truncated, Data: data}, 200, nil
}
