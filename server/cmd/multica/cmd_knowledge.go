package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var knowledgeCmd = &cobra.Command{
	Use:   "knowledge",
	Short: "Read knowledge bases and their indexed sources",
}

var knowledgeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List knowledge bases in the workspace",
	RunE:  runKnowledgeList,
}

var knowledgeDocumentCmd = &cobra.Command{Use: "document", Short: "Read knowledge-base documents"}
var knowledgeDocumentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List documents in a knowledge base",
	RunE:  runKnowledgeDocumentList,
}

var knowledgeSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search a knowledge base",
	RunE:  runKnowledgeSearch,
}

var knowledgeReadCmd = &cobra.Command{
	Use:   "read",
	Short: "Read one indexed chunk",
	RunE:  runKnowledgeRead,
}

var knowledgeEntityCmd = &cobra.Command{Use: "entity", Short: "Read knowledge entities"}
var knowledgeEntityListCmd = &cobra.Command{
	Use:   "list",
	Short: "List entities in a knowledge base",
	RunE:  runKnowledgeEntityList,
}
var knowledgeEntityGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Read one knowledge entity and its evidence",
	RunE:  runKnowledgeEntityGet,
}

var knowledgeGraphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Read a bounded knowledge graph",
	RunE:  runKnowledgeGraph,
}

func init() {
	knowledgeCmd.AddCommand(knowledgeListCmd, knowledgeSearchCmd, knowledgeReadCmd, knowledgeGraphCmd)
	knowledgeCmd.AddCommand(knowledgeDocumentCmd, knowledgeEntityCmd)
	knowledgeDocumentCmd.AddCommand(knowledgeDocumentListCmd)
	knowledgeEntityCmd.AddCommand(knowledgeEntityListCmd, knowledgeEntityGetCmd)

	knowledgeListCmd.Flags().String("output", "table", "Output format: table or json")
	knowledgeDocumentListCmd.Flags().String("base", "", "Knowledge base ID (required)")
	knowledgeDocumentListCmd.Flags().String("output", "table", "Output format: table or json")
	knowledgeSearchCmd.Flags().String("base", "", "Knowledge base ID (required)")
	knowledgeSearchCmd.Flags().String("query", "", "Search query (required)")
	knowledgeSearchCmd.Flags().Int("limit", 10, "Maximum number of results")
	knowledgeSearchCmd.Flags().String("mode", "hybrid", "Search mode: hybrid, keyword, or semantic")
	knowledgeSearchCmd.Flags().String("output", "table", "Output format: table or json")
	knowledgeReadCmd.Flags().String("base", "", "Knowledge base ID (required)")
	knowledgeReadCmd.Flags().String("chunk", "", "Chunk ID (required)")
	knowledgeReadCmd.Flags().String("output", "table", "Output format: table or json")
	knowledgeEntityListCmd.Flags().String("base", "", "Knowledge base ID (required)")
	knowledgeEntityListCmd.Flags().String("query", "", "Filter by entity name")
	knowledgeEntityListCmd.Flags().String("output", "table", "Output format: table or json")
	knowledgeEntityGetCmd.Flags().String("base", "", "Knowledge base ID (required)")
	knowledgeEntityGetCmd.Flags().String("id", "", "Entity ID (required)")
	knowledgeEntityGetCmd.Flags().String("output", "table", "Output format: table or json")
	knowledgeGraphCmd.Flags().String("base", "", "Knowledge base ID (required)")
	knowledgeGraphCmd.Flags().String("entity", "", "Seed entity ID")
	knowledgeGraphCmd.Flags().Int("depth", 1, "Traversal depth: 1 or 2")
	knowledgeGraphCmd.Flags().String("output", "table", "Output format: table or json")
}

func knowledgeClient(cmd *cobra.Command) (*cli.APIClient, context.Context, context.CancelFunc, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := cli.APIContext(context.Background())
	return client, ctx, cancel, nil
}

func knowledgeBaseFlag(cmd *cobra.Command) (string, error) {
	base, _ := cmd.Flags().GetString("base")
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("--base is required")
	}
	return strings.TrimSpace(base), nil
}

func knowledgeOutput(cmd *cobra.Command, value any, headers []string, rows [][]string) error {
	format, _ := cmd.Flags().GetString("output")
	if format == "json" {
		return cli.PrintJSON(os.Stdout, value)
	}
	if format != "table" {
		return fmt.Errorf("invalid --output %q; use table or json", format)
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func knowledgePath(parts ...string) string {
	path := "/api/knowledge"
	for _, part := range parts {
		path += "/" + url.PathEscape(strings.TrimSpace(part))
	}
	return path
}

func runKnowledgeList(cmd *cobra.Command, _ []string) error {
	client, ctx, cancel, err := knowledgeClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var response struct {
		Bases []map[string]any `json:"bases"`
	}
	if err := client.GetJSON(ctx, knowledgePath("bases"), &response); err != nil {
		return fmt.Errorf("list knowledge bases: %w", err)
	}
	rows := make([][]string, 0, len(response.Bases))
	for _, base := range response.Bases {
		rows = append(rows, []string{strVal(base, "id"), strVal(base, "name"), strVal(base, "visibility"), strVal(base, "revision"), strVal(base, "corpus_revision")})
	}
	return knowledgeOutput(cmd, response, []string{"ID", "NAME", "VISIBILITY", "REVISION", "CORPUS"}, rows)
}

func runKnowledgeDocumentList(cmd *cobra.Command, _ []string) error {
	base, err := knowledgeBaseFlag(cmd)
	if err != nil {
		return err
	}
	client, ctx, cancel, err := knowledgeClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var response struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := client.GetJSON(ctx, knowledgePath("bases", base, "documents"), &response); err != nil {
		return fmt.Errorf("list knowledge documents: %w", err)
	}
	rows := make([][]string, 0, len(response.Documents))
	for _, document := range response.Documents {
		rows = append(rows, []string{strVal(document, "id"), strVal(document, "title"), strVal(document, "source_kind"), strVal(document, "status"), strVal(document, "revision")})
	}
	return knowledgeOutput(cmd, response, []string{"ID", "TITLE", "SOURCE", "STATUS", "REVISION"}, rows)
}

func runKnowledgeSearch(cmd *cobra.Command, _ []string) error {
	base, err := knowledgeBaseFlag(cmd)
	if err != nil {
		return err
	}
	query, _ := cmd.Flags().GetString("query")
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("--query is required")
	}
	limit, _ := cmd.Flags().GetInt("limit")
	mode, _ := cmd.Flags().GetString("mode")
	client, ctx, cancel, err := knowledgeClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var response map[string]any
	if err := client.PostJSON(ctx, knowledgePath("bases", base, "search"), map[string]any{"query": query, "limit": limit, "mode": mode}, &response); err != nil {
		return fmt.Errorf("search knowledge base: %w", err)
	}
	results, _ := response["results"].([]any)
	rows := make([][]string, 0, len(results))
	for _, raw := range results {
		result, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		text := strVal(result, "text")
		if len([]rune(text)) > 120 {
			text = string([]rune(text)[:120]) + "…"
		}
		rows = append(rows, []string{strVal(result, "rank"), strVal(result, "title"), strVal(result, "document_id"), strVal(result, "chunk_id"), text})
	}
	return knowledgeOutput(cmd, response, []string{"RANK", "TITLE", "DOCUMENT", "CHUNK", "TEXT"}, rows)
}

func runKnowledgeRead(cmd *cobra.Command, _ []string) error {
	base, err := knowledgeBaseFlag(cmd)
	if err != nil {
		return err
	}
	chunk, _ := cmd.Flags().GetString("chunk")
	if strings.TrimSpace(chunk) == "" {
		return fmt.Errorf("--chunk is required")
	}
	client, ctx, cancel, err := knowledgeClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var response map[string]any
	if err := client.GetJSON(ctx, knowledgePath("bases", base, "chunks", chunk), &response); err != nil {
		return fmt.Errorf("read knowledge chunk: %w", err)
	}
	return knowledgeOutput(cmd, response, []string{"ID", "DOCUMENT", "VERSION", "ORDINAL", "TEXT"}, [][]string{{strVal(response, "id"), strVal(response, "document_id"), strVal(response, "version_id"), strVal(response, "ordinal"), strVal(response, "text")}})
}

func runKnowledgeEntityList(cmd *cobra.Command, _ []string) error {
	base, err := knowledgeBaseFlag(cmd)
	if err != nil {
		return err
	}
	query, _ := cmd.Flags().GetString("query")
	client, ctx, cancel, err := knowledgeClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	path := knowledgePath("bases", base, "entities")
	if strings.TrimSpace(query) != "" {
		path += "?" + url.Values{"query": []string{query}}.Encode()
	}
	var response struct {
		Entities []map[string]any `json:"entities"`
	}
	if err := client.GetJSON(ctx, path, &response); err != nil {
		return fmt.Errorf("list knowledge entities: %w", err)
	}
	rows := make([][]string, 0, len(response.Entities))
	for _, entity := range response.Entities {
		rows = append(rows, []string{strVal(entity, "id"), strVal(entity, "type"), strVal(entity, "canonical_name"), strVal(entity, "review_status"), strVal(entity, "evidence_count")})
	}
	return knowledgeOutput(cmd, response, []string{"ID", "TYPE", "NAME", "STATUS", "EVIDENCE"}, rows)
}

func runKnowledgeEntityGet(cmd *cobra.Command, _ []string) error {
	base, err := knowledgeBaseFlag(cmd)
	if err != nil {
		return err
	}
	entityID, _ := cmd.Flags().GetString("id")
	if strings.TrimSpace(entityID) == "" {
		return fmt.Errorf("--id is required")
	}
	client, ctx, cancel, err := knowledgeClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var response map[string]any
	if err := client.GetJSON(ctx, knowledgePath("bases", base, "entities", entityID), &response); err != nil {
		return fmt.Errorf("get knowledge entity: %w", err)
	}
	entity, _ := response["entity"].(map[string]any)
	return knowledgeOutput(cmd, response, []string{"ID", "TYPE", "NAME", "STATUS", "EVIDENCE"}, [][]string{{strVal(entity, "id"), strVal(entity, "type"), strVal(entity, "canonical_name"), strVal(entity, "review_status"), strVal(entity, "evidence_count")}})
}

func runKnowledgeGraph(cmd *cobra.Command, _ []string) error {
	base, err := knowledgeBaseFlag(cmd)
	if err != nil {
		return err
	}
	entityID, _ := cmd.Flags().GetString("entity")
	depth, _ := cmd.Flags().GetInt("depth")
	if depth < 1 || depth > 2 {
		return fmt.Errorf("--depth must be 1 or 2")
	}
	client, ctx, cancel, err := knowledgeClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	query := url.Values{"depth": []string{strconv.Itoa(depth)}}
	if strings.TrimSpace(entityID) != "" {
		query.Set("entity_id", entityID)
	}
	var response map[string]any
	if err := client.GetJSON(ctx, knowledgePath("bases", base, "graph")+"?"+query.Encode(), &response); err != nil {
		return fmt.Errorf("read knowledge graph: %w", err)
	}
	if format, _ := cmd.Flags().GetString("output"); format == "json" {
		return cli.PrintJSON(os.Stdout, response)
	}
	nodes, _ := response["nodes"].([]any)
	nodeRows := make([][]string, 0, len(nodes))
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if ok {
			nodeRows = append(nodeRows, []string{strVal(node, "id"), strVal(node, "type"), strVal(node, "canonical_name"), strVal(node, "evidence_count")})
		}
	}
	cli.PrintTable(os.Stdout, []string{"NODE ID", "TYPE", "NAME", "EVIDENCE"}, nodeRows)
	relations, _ := response["relations"].([]any)
	relationRows := make([][]string, 0, len(relations))
	for _, raw := range relations {
		relation, ok := raw.(map[string]any)
		if ok {
			relationRows = append(relationRows, []string{strVal(relation, "source_entity_id"), strVal(relation, "predicate"), strVal(relation, "target_entity_id"), strVal(relation, "review_status")})
		}
	}
	cli.PrintTable(os.Stdout, []string{"SOURCE", "PREDICATE", "TARGET", "STATUS"}, relationRows)
	return nil
}
