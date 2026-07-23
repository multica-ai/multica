package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/cerebro/grantrecovery"
)

type options struct {
	sourceURL   string
	targetURL   string
	workspaceID string
	apply       bool
	approvalID  string
}

const recoveryApprovalQuery = `
	UPDATE cerebro_approval_request AS approval
	SET consumed_at = now(), updated_at = now()
	FROM member AS approver
	WHERE approval.id = $1
	  AND approval.workspace_id = $2
	  AND approval.agent_id IS NULL
	  AND approval.capability = $3
	  AND approval.resource = $4
	  AND approval.context ->> 'issue_id' = $5
	  AND approval.context ->> 'approval_boundary' = $6
	  AND approval.status = 'approved'
	  AND approval.single_use = TRUE
	  AND approval.consumed_at IS NULL
	  AND (approval.expires_at IS NULL OR approval.expires_at > now())
	  AND approval.decided_by_id = approver.user_id
	  AND approver.workspace_id = approval.workspace_id
	  AND approver.role IN ('owner', 'admin')
	RETURNING approval.decided_by_id::text
`

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "permission recovery:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("cerebro_permission_recovery", flag.ContinueOnError)
	var opts options
	flags.StringVar(&opts.sourceURL, "source-url", os.Getenv("RECOVERY_SOURCE_DATABASE_URL"), "read-only PITR database URL")
	flags.StringVar(&opts.targetURL, "target-url", os.Getenv("DATABASE_URL"), "current database URL")
	flags.StringVar(&opts.workspaceID, "workspace-id", "", "workspace UUID to reconcile")
	flags.BoolVar(&opts.apply, "apply", false, "apply a safe diff; default is dry-run")
	flags.StringVar(&opts.approvalID, "approval-id", "", "single-use production recovery approval UUID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := validateOptions(opts); err != nil {
		return err
	}

	sourceConfig, err := pgxpool.ParseConfig(opts.sourceURL)
	if err != nil {
		return fmt.Errorf("parse source URL: %w", err)
	}
	if sourceConfig.ConnConfig.RuntimeParams == nil {
		sourceConfig.ConnConfig.RuntimeParams = map[string]string{}
	}
	sourceConfig.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	source, err := pgxpool.NewWithConfig(ctx, sourceConfig)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer source.Close()
	target, err := pgxpool.New(ctx, opts.targetURL)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	defer target.Close()
	if err := requireSeparateDatabases(ctx, source, target); err != nil {
		return err
	}

	grants, err := loadLegacyGrants(ctx, source, opts.workspaceID)
	if err != nil {
		return err
	}
	grants, err = canonicalizeLegacyGrants(ctx, target, grants)
	if err != nil {
		return err
	}
	existing, agents, err := loadTargetState(ctx, target, opts.workspaceID)
	if err != nil {
		return err
	}
	diff := grantrecovery.BuildDiff(grants, existing, agents)
	if err := printJSON(diff); err != nil {
		return err
	}
	if !opts.apply {
		return nil
	}
	if !diff.SafeToApply() {
		return fmt.Errorf("apply refused: %d conflicting and %d unmapped rows", len(diff.Conflicting), len(diff.Unmapped))
	}
	return apply(ctx, target, opts, grants)
}

func validateOptions(opts options) error {
	if strings.TrimSpace(opts.sourceURL) == "" || strings.TrimSpace(opts.targetURL) == "" || strings.TrimSpace(opts.workspaceID) == "" {
		return errors.New("source-url, target-url, and workspace-id are required")
	}
	if opts.sourceURL == opts.targetURL {
		return errors.New("source and target database URLs must be different")
	}
	if opts.apply && strings.TrimSpace(opts.approvalID) == "" {
		return errors.New("apply requires approval-id")
	}
	return nil
}

func loadLegacyGrants(ctx context.Context, source *pgxpool.Pool, workspaceID string) ([]grantrecovery.LegacyGrant, error) {
	rows, err := source.Query(ctx, `
		SELECT ag.workspace_id::text, atg.agent_id::text, atg.tool_name, atg.enabled, atg.config_json
		FROM agent_tool_grant atg
		JOIN agent ag ON ag.id = atg.agent_id
		WHERE ag.workspace_id = $1
		ORDER BY atg.agent_id, atg.tool_name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("read legacy grants from PITR source: %w", err)
	}
	defer rows.Close()
	var grants []grantrecovery.LegacyGrant
	for rows.Next() {
		var grant grantrecovery.LegacyGrant
		var config []byte
		if err := rows.Scan(&grant.WorkspaceID, &grant.AgentID, &grant.ToolName, &grant.Enabled, &config); err != nil {
			return nil, fmt.Errorf("scan legacy grant: %w", err)
		}
		grant.Config = config
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read legacy grants: %w", err)
	}
	return grants, nil
}

func canonicalizeLegacyGrants(ctx context.Context, target identityQuerier, grants []grantrecovery.LegacyGrant) ([]grantrecovery.LegacyGrant, error) {
	canonical := make([]grantrecovery.LegacyGrant, len(grants))
	copy(canonical, grants)
	for index := range canonical {
		var toolName *string
		err := target.QueryRow(ctx, `SELECT cerebro_canonical_policy_tool_key($1)`, canonical[index].ToolName).Scan(&toolName)
		if err != nil {
			return nil, fmt.Errorf("resolve canonical capability for %q: %w", canonical[index].ToolName, err)
		}
		if toolName == nil || strings.TrimSpace(*toolName) == "" {
			canonical[index].ToolName = ""
			continue
		}
		canonical[index].ToolName = *toolName
	}
	return canonical, nil
}

type targetQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type identityQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func requireSeparateDatabases(ctx context.Context, source, target identityQuerier) error {
	sourceIdentity, err := databaseIdentity(ctx, source)
	if err != nil {
		return fmt.Errorf("identify source database: %w", err)
	}
	targetIdentity, err := databaseIdentity(ctx, target)
	if err != nil {
		return fmt.Errorf("identify target database: %w", err)
	}
	if sourceIdentity == targetIdentity {
		return errors.New("source and target connections resolve to the same physical database")
	}
	return nil
}

func databaseIdentity(ctx context.Context, queryer identityQuerier) (string, error) {
	var serverAddress, serverPort, databaseID string
	err := queryer.QueryRow(ctx, `
		SELECT COALESCE(inet_server_addr()::text, 'local-socket'),
		       inet_server_port()::text,
		       oid::text
		FROM pg_database
		WHERE datname = current_database()
	`).Scan(&serverAddress, &serverPort, &databaseID)
	if err != nil {
		return "", err
	}
	return serverAddress + ":" + serverPort + ":" + databaseID, nil
}

func loadTargetState(ctx context.Context, target targetQuerier, workspaceID string) ([]grantrecovery.Rule, map[string]bool, error) {
	agentRows, err := target.Query(ctx, `SELECT id::text FROM agent WHERE workspace_id = $1`, workspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("read target agents: %w", err)
	}
	agents := map[string]bool{}
	for agentRows.Next() {
		var id string
		if err := agentRows.Scan(&id); err != nil {
			agentRows.Close()
			return nil, nil, err
		}
		agents[id] = true
	}
	agentRows.Close()
	if err := agentRows.Err(); err != nil {
		return nil, nil, err
	}

	rows, err := target.Query(ctx, `
		SELECT workspace_id::text, subject_id::text, tool_key, setting, resource_pattern, conditions
		FROM cerebro_tool_policy
		WHERE workspace_id = $1 AND layer = 'agent'
	`, workspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("read target policies: %w", err)
	}
	defer rows.Close()
	var rules []grantrecovery.Rule
	for rows.Next() {
		var rule grantrecovery.Rule
		var conditions []byte
		if err := rows.Scan(&rule.WorkspaceID, &rule.AgentID, &rule.ToolKey, &rule.Setting, &rule.ResourcePattern, &conditions); err != nil {
			return nil, nil, err
		}
		rule.Conditions = conditions
		rules = append(rules, rule)
	}
	return rules, agents, rows.Err()
}

func apply(ctx context.Context, target *pgxpool.Pool, opts options, grants []grantrecovery.LegacyGrant) error {
	tx, err := target.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "permission-recovery:"+opts.workspaceID); err != nil {
		return err
	}
	existing, agents, err := loadTargetState(ctx, tx, opts.workspaceID)
	if err != nil {
		return err
	}
	diff := grantrecovery.BuildDiff(grants, existing, agents)
	if !diff.SafeToApply() {
		return fmt.Errorf("target changed: %d conflicting and %d unmapped rows", len(diff.Conflicting), len(diff.Unmapped))
	}
	approvedBy, err := consumeRecoveryApproval(ctx, tx, opts, diff.SourceFingerprint)
	if err != nil {
		return err
	}

	identities := make([]string, 0, len(diff.Mapped))
	for _, rule := range diff.Mapped {
		result, err := tx.Exec(ctx, `
			INSERT INTO cerebro_tool_policy (
				workspace_id, tool_key, layer, subject_id, setting, resource_pattern, conditions, updated_by
			) VALUES ($1, $2, 'agent', $3, $4, $5, $6, $7)
			ON CONFLICT (workspace_id, tool_key, layer, subject_id, resource_pattern) DO NOTHING
		`, rule.WorkspaceID, rule.ToolKey, rule.AgentID, rule.Setting, rule.ResourcePattern, rule.Conditions, approvedBy)
		if err != nil {
			return fmt.Errorf("insert %s/%s: %w", rule.AgentID, rule.ToolKey, err)
		}
		if result.RowsAffected() != 1 {
			return fmt.Errorf("insert %s/%s raced with another policy change", rule.AgentID, rule.ToolKey)
		}
		identities = append(identities, strings.Join([]string{rule.AgentID, rule.ToolKey, rule.ResourcePattern}, ":"))
	}
	identityJSON, _ := json.Marshal(identities)
	if _, err := tx.Exec(ctx, `
		INSERT INTO cerebro_permission_recovery_audit (
			workspace_id, source_fingerprint, approval_id, approved_by,
			imported_count, already_present_count, imported_identities
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (workspace_id, source_fingerprint) DO NOTHING
	`, opts.workspaceID, diff.SourceFingerprint, opts.approvalID, approvedBy, len(diff.Mapped), len(diff.AlreadyPresent), identityJSON); err != nil {
		return fmt.Errorf("write recovery audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "applied %d policy rows at %s\n", len(diff.Mapped), time.Now().UTC().Format(time.RFC3339))
	return nil
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func consumeRecoveryApproval(ctx context.Context, queryer rowQuerier, opts options, fingerprint string) (string, error) {
	var approvedBy string
	err := queryer.QueryRow(
		ctx,
		recoveryApprovalQuery,
		opts.approvalID,
		opts.workspaceID,
		grantrecovery.ApprovalCapability,
		grantrecovery.ApprovalResource(opts.workspaceID, fingerprint),
		grantrecovery.ApprovalIssueID,
		grantrecovery.ApprovalBoundary,
	).Scan(&approvedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("apply refused: approval must be a live, unused, single-use recovery approval decided by a workspace owner or admin")
	}
	if err != nil {
		return "", fmt.Errorf("consume recovery approval: %w", err)
	}
	return approvedBy, nil
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
