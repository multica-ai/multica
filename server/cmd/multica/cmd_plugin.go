package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

var pluginCmd = &cobra.Command{Use: "plugin", Short: "Develop and manage Plugins"}

var pluginInitCmd = &cobra.Command{
	Use:   "init <dir>",
	Short: "Create a minimal private Skill Plugin",
	Args:  exactArgs(1),
	RunE:  runPluginInit,
}

var pluginValidateCmd = &cobra.Command{
	Use:   "validate <dir|archive>",
	Short: "Validate a Plugin source directory or ZIP",
	Args:  exactArgs(1),
	RunE:  runPluginValidate,
}

var pluginPackCmd = &cobra.Command{
	Use:   "pack <dir>",
	Short: "Create a deterministic Plugin ZIP",
	Args:  exactArgs(1),
	RunE:  runPluginPack,
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install <dir|archive>",
	Short: "Install or update a workspace-private Plugin",
	Args:  exactArgs(1),
	RunE:  runPluginInstall,
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workspace-private Plugin installations",
	Args:  cobra.NoArgs,
	RunE:  runPluginList,
}

var pluginStatusCmd = &cobra.Command{
	Use:   "status [installation-id|plugin-key]",
	Short: "Show workspace-private Plugin status",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runPluginStatus,
}

var pluginRemoteMCPCmd = &cobra.Command{Use: "remote-mcp", Short: "Configure and review Remote MCP contributions"}

var pluginRemoteMCPConfigureCmd = &cobra.Command{
	Use: "configure <installation-id> <contribution-key>", Short: "Configure and test a Remote MCP endpoint", Args: exactArgs(2), RunE: runPluginRemoteMCPConfigure,
}
var pluginRemoteMCPTestCmd = &cobra.Command{
	Use: "test <installation-id> <contribution-key>", Short: "Test and discover a configured Remote MCP endpoint", Args: exactArgs(2), RunE: runPluginRemoteMCPTest,
}
var pluginRemoteMCPApproveCmd = &cobra.Command{
	Use: "approve <installation-id> <contribution-key>", Short: "Approve an explicit Remote MCP tool allowlist", Args: exactArgs(2), RunE: runPluginRemoteMCPApprove,
}
var pluginRemoteMCPRevokeCmd = &cobra.Command{
	Use: "revoke <installation-id> <contribution-key>", Short: "Revoke the Remote MCP credential and disable the Plugin", Args: exactArgs(2), RunE: runPluginRemoteMCPRevoke,
}

func init() {
	pluginCmd.AddCommand(pluginInitCmd, pluginValidateCmd, pluginPackCmd, pluginInstallCmd, pluginListCmd, pluginStatusCmd, pluginRemoteMCPCmd)
	pluginRemoteMCPCmd.AddCommand(pluginRemoteMCPConfigureCmd, pluginRemoteMCPTestCmd, pluginRemoteMCPApproveCmd, pluginRemoteMCPRevokeCmd)
	pluginInitCmd.Flags().String("key", "dev.local.example-skill", "Reverse-DNS Plugin key")
	pluginInitCmd.Flags().String("name", "Example Skill", "Plugin and Skill display name")
	pluginInitCmd.Flags().String("publisher", "private.local", "Private publisher label")
	pluginInitCmd.Flags().String("contribution", "skill", "Contribution template: skill or remote-mcp")
	pluginInitCmd.Flags().String("endpoint-host", "mcp.example.com", "Allowed Remote MCP endpoint host for the remote-mcp template")
	pluginValidateCmd.Flags().String("output", "json", "Output format: table or json")
	pluginPackCmd.Flags().String("output", "", "Output ZIP path (required)")
	pluginPackCmd.Flags().String("format", "table", "Result format: table or json")
	pluginInstallCmd.Flags().String("workspace", "", "Workspace ID, slug, or ID prefix")
	pluginInstallCmd.Flags().String("output", "json", "Output format: table or json")
	pluginListCmd.Flags().String("workspace", "", "Workspace ID, slug, or ID prefix")
	pluginListCmd.Flags().String("output", "table", "Output format: table or json")
	pluginStatusCmd.Flags().String("workspace", "", "Workspace ID, slug, or ID prefix")
	pluginStatusCmd.Flags().String("output", "json", "Output format: table or json")
	for _, command := range []*cobra.Command{pluginRemoteMCPConfigureCmd, pluginRemoteMCPTestCmd, pluginRemoteMCPApproveCmd, pluginRemoteMCPRevokeCmd} {
		command.Flags().String("workspace", "", "Workspace ID, slug, or ID prefix")
		command.Flags().String("output", "json", "Output format: json")
	}
	pluginRemoteMCPConfigureCmd.Flags().String("endpoint", "", "Public HTTPS MCP endpoint (required)")
	pluginRemoteMCPConfigureCmd.Flags().String("auth-type", "none", "Authentication: none, bearer, or header")
	pluginRemoteMCPConfigureCmd.Flags().String("auth-header", "", "Header name when --auth-type=header")
	pluginRemoteMCPConfigureCmd.Flags().Bool("credential-stdin", false, "Read the credential from stdin; keeps it out of shell history and ps")
	pluginRemoteMCPConfigureCmd.Flags().String("credential-file", "", "Read the credential from a file (suggested mode: 0600)")
	pluginRemoteMCPConfigureCmd.Flags().String("public-config-file", "", "Read non-secret public configuration JSON from a file")
	pluginRemoteMCPConfigureCmd.Flags().String("failure-policy", "required", "Failure policy: required or optional")
	pluginRemoteMCPApproveCmd.Flags().StringSlice("tool", nil, "Tool name to approve (repeatable or comma-separated; required)")
}

func runPluginInit(cmd *cobra.Command, args []string) error {
	dir := filepath.Clean(args[0])
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) > 0 {
		return fmt.Errorf("plugin directory %q is not empty", dir)
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect plugin directory: %w", err)
	}
	key, _ := cmd.Flags().GetString("key")
	name, _ := cmd.Flags().GetString("name")
	publisher, _ := cmd.Flags().GetString("publisher")
	contributionType, _ := cmd.Flags().GetString("contribution")
	endpointHost, _ := cmd.Flags().GetString("endpoint-host")
	if contributionType == "" {
		contributionType = "skill"
	}
	if endpointHost == "" {
		endpointHost = "mcp.example.com"
	}
	manifest := plugincontract.Manifest{
		APIVersion: plugincontract.APIVersionV1,
		Kind:       plugincontract.KindPlugin,
		Metadata: plugincontract.Metadata{
			Key: key, Name: name, Description: "A workspace-private declarative Skill Plugin.", Version: "0.1.0", Publisher: publisher,
		},
		Compatibility: plugincontract.Compatibility{HostAPI: ">=1.0.0 <2.0.0"},
	}
	switch contributionType {
	case "skill":
		manifest.Compatibility.RequiredDaemonFeatures = []string{plugincontract.DaemonFeatureExecutionManifestV1, plugincontract.DaemonFeatureAgentSkillV1}
		manifest.RequestedCapabilities = []string{plugincontract.CapabilityAgentSkillContribute}
		manifest.Contributes.AgentSkills = []plugincontract.AgentSkillContribution{{
			Key: "example-skill", Name: name, Description: "Explain when and how this Skill should be used.", Entry: "skills/example-skill/SKILL.md",
		}}
	case "remote-mcp":
		manifest.Metadata.Description = "A workspace-private declarative Remote MCP Plugin."
		manifest.Compatibility.RequiredDaemonFeatures = []string{plugincontract.DaemonFeatureExecutionManifestV1, plugincontract.DaemonFeatureRemoteMCPV1}
		manifest.RequestedCapabilities = []string{plugincontract.CapabilityRemoteMCPConnect}
		manifest.Contributes.RemoteMCP = []plugincontract.RemoteMCPContribution{{
			Key: "example-tools", Name: name, Description: "Expose explicitly reviewed remote tools.",
			Transport: "streamable-http", ProtocolVersions: []string{"2025-03-26", "2024-11-05"},
			EndpointPolicy: plugincontract.RemoteMCPEndpointPolicy{
				DefaultEndpoint: "https://" + endpointHost + "/mcp", AllowedHosts: []string{endpointHost},
			},
			Authentication: plugincontract.RemoteMCPAuthentication{
				Preferred: "oauth", Supported: []string{"oauth", "bearer", "header", "none"},
			},
			ToolIntent:          []plugincontract.RemoteMCPToolIntent{{Name: "example.read", Description: "Read an example value.", Risk: "read"}},
			ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		}}
	default:
		return fmt.Errorf("--contribution must be skill or remote-mcp")
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestBytes = append(manifestBytes, '\n')
	if _, _, err := plugincontract.ParseManifest(manifestBytes); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, plugincontract.ManifestFilename), manifestBytes, 0o644); err != nil {
		return err
	}
	if contributionType == "skill" {
		if err := os.MkdirAll(filepath.Join(dir, "skills", "example-skill"), 0o755); err != nil {
			return err
		}
		skill := []byte("---\nname: example-skill\ndescription: Explain when this Skill should be used.\n---\n\n# Example Skill\n\nDescribe the declarative instructions the Agent should follow.\n")
		if err := os.WriteFile(filepath.Join(dir, "skills", "example-skill", "SKILL.md"), skill, 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Initialized Private Plugin in %s\n", dir)
	return nil
}

type pluginDigestOutput struct {
	PluginKey      string `json:"plugin_key"`
	Version        string `json:"version"`
	ManifestDigest string `json:"manifest_digest"`
	ArchiveDigest  string `json:"archive_digest"`
	ArtifactDigest string `json:"artifact_digest"`
	SizeBytes      int64  `json:"size_bytes"`
	FileCount      int    `json:"file_count"`
}

func loadPluginSource(source string) ([]byte, plugincontract.Artifact, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, plugincontract.Artifact{}, err
	}
	if info.IsDir() {
		return plugincontract.PackDirectory(source)
	}
	if !info.Mode().IsRegular() {
		return nil, plugincontract.Artifact{}, fmt.Errorf("plugin source %q is not a directory or regular ZIP", source)
	}
	if info.Size() > plugincontract.MaxArchiveSize {
		return nil, plugincontract.Artifact{}, fmt.Errorf("plugin archive exceeds %d bytes", plugincontract.MaxArchiveSize)
	}
	archive, err := os.ReadFile(source)
	if err != nil {
		return nil, plugincontract.Artifact{}, err
	}
	artifact, err := plugincontract.ValidateArtifact(archive)
	return archive, artifact, err
}

func pluginDigestResult(artifact plugincontract.Artifact) pluginDigestOutput {
	return pluginDigestOutput{
		PluginKey: artifact.Manifest.Metadata.Key, Version: artifact.Manifest.Metadata.Version,
		ManifestDigest: plugincontract.DigestBytes(artifact.CanonicalManifest), ArchiveDigest: artifact.ArchiveDigest,
		ArtifactDigest: artifact.ArtifactDigest, SizeBytes: artifact.SizeBytes, FileCount: len(artifact.Files),
	}
}

func printPluginDigest(cmd *cobra.Command, result pluginDigestOutput, output string) error {
	if output == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), result)
	}
	cli.PrintTable(cmd.OutOrStdout(), []string{"PLUGIN", "VERSION", "MANIFEST", "ARCHIVE", "ARTIFACT"}, [][]string{{
		result.PluginKey, result.Version, result.ManifestDigest, result.ArchiveDigest, result.ArtifactDigest,
	}})
	return nil
}

func runPluginValidate(cmd *cobra.Command, args []string) error {
	_, artifact, err := loadPluginSource(args[0])
	if err != nil {
		return fmt.Errorf("validate Plugin: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printPluginDigest(cmd, pluginDigestResult(artifact), output)
}

func runPluginPack(cmd *cobra.Command, args []string) error {
	outputPath, _ := cmd.Flags().GetString("output")
	if outputPath == "" {
		return fmt.Errorf("--output is required")
	}
	archive, artifact, err := plugincontract.PackDirectory(args[0])
	if err != nil {
		return fmt.Errorf("pack Plugin: %w", err)
	}
	if err := os.WriteFile(filepath.Clean(outputPath), archive, 0o644); err != nil {
		return fmt.Errorf("write Plugin archive: %w", err)
	}
	format, _ := cmd.Flags().GetString("format")
	return printPluginDigest(cmd, pluginDigestResult(artifact), format)
}

func pluginWorkspaceID(cmd *cobra.Command) (string, error) {
	workspace, _ := cmd.Flags().GetString("workspace")
	if workspace == "" {
		return requireWorkspaceID(cmd)
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	resolved, err := resolveWorkspaceRef(ctx, cmd, workspace)
	if err != nil {
		return "", err
	}
	return resolved.ID, nil
}

func runPluginInstall(cmd *cobra.Command, args []string) error {
	if err := requireHumanLocalCommand("plugin install"); err != nil {
		return err
	}
	archive, artifact, err := loadPluginSource(args[0])
	if err != nil {
		return fmt.Errorf("validate Plugin before install: %w", err)
	}
	workspaceID, err := pluginWorkspaceID(cmd)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	client.WorkspaceID = workspaceID
	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(time.Minute))
	defer cancel()
	var result map[string]any
	path := "/api/workspaces/" + workspaceID + "/plugins/private/install"
	if err := client.UploadPrivatePlugin(ctx, path, archive, artifact.Manifest.Metadata.Key+"-"+artifact.Manifest.Metadata.Version+".zip", &result); err != nil {
		return fmt.Errorf("install Private Plugin: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), result)
	}
	return printPluginRows(cmd, []map[string]any{result})
}

func fetchPrivatePlugins(cmd *cobra.Command) ([]map[string]any, error) {
	workspaceID, err := pluginWorkspaceID(cmd)
	if err != nil {
		return nil, err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	client.WorkspaceID = workspaceID
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var response struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces/"+workspaceID+"/plugins/private", &response); err != nil {
		return nil, err
	}
	return response.Plugins, nil
}

func printPluginRows(cmd *cobra.Command, plugins []map[string]any) error {
	rows := make([][]string, 0, len(plugins))
	for _, plugin := range plugins {
		rows = append(rows, []string{
			strVal(plugin, "plugin_key"), strVal(plugin, "desired_version"), strVal(plugin, "lifecycle_status"),
			strVal(plugin, "trust_tier"), strVal(plugin, "uploader_id"),
		})
	}
	cli.PrintTable(cmd.OutOrStdout(), []string{"PLUGIN", "VERSION", "STATUS", "TRUST", "UPLOADER"}, rows)
	return nil
}

func runPluginList(cmd *cobra.Command, _ []string) error {
	plugins, err := fetchPrivatePlugins(cmd)
	if err != nil {
		return fmt.Errorf("list Private Plugins: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), plugins)
	}
	return printPluginRows(cmd, plugins)
}

func runPluginStatus(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		workspaceID, err := pluginWorkspaceID(cmd)
		if err != nil {
			return err
		}
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		client.WorkspaceID = workspaceID
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()
		var plugin map[string]any
		if err := client.GetJSON(ctx, "/api/workspaces/"+workspaceID+"/plugins/private/"+url.PathEscape(args[0]), &plugin); err != nil {
			return fmt.Errorf("get Private Plugin status: %w", err)
		}
		output, _ := cmd.Flags().GetString("output")
		if output == "json" {
			return cli.PrintJSON(cmd.OutOrStdout(), plugin)
		}
		return printPluginRows(cmd, []map[string]any{plugin})
	}
	plugins, err := fetchPrivatePlugins(cmd)
	if err != nil {
		return fmt.Errorf("get Private Plugin status: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), plugins)
	}
	return printPluginRows(cmd, plugins)
}

func remoteMCPAPI(cmd *cobra.Command, args []string, suffix string, method string, body any) error {
	if err := requireHumanLocalCommand("plugin remote-mcp " + method); err != nil {
		return err
	}
	workspaceID, err := pluginWorkspaceID(cmd)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	client.WorkspaceID = workspaceID
	path := "/api/workspaces/" + workspaceID + "/plugins/" + url.PathEscape(args[0]) + "/remote-mcp/" + url.PathEscape(args[1]) + suffix
	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(time.Minute))
	defer cancel()
	if method == "revoke" {
		if err := client.DeleteJSON(ctx, path); err != nil {
			return err
		}
		return cli.PrintJSON(cmd.OutOrStdout(), map[string]any{"revoked": true, "plugin_disabled": true})
	}
	var result map[string]any
	if suffix == "/config" {
		err = client.PutJSON(ctx, path, body, &result)
	} else {
		err = client.PostJSON(ctx, path, body, &result)
	}
	if err != nil {
		return err
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}

func runPluginRemoteMCPConfigure(cmd *cobra.Command, args []string) error {
	endpoint, _ := cmd.Flags().GetString("endpoint")
	if endpoint == "" {
		return fmt.Errorf("--endpoint is required")
	}
	credential, err := readRemoteMCPCredential(cmd)
	if err != nil {
		return err
	}
	publicConfig := json.RawMessage(`{}`)
	if configPath, _ := cmd.Flags().GetString("public-config-file"); configPath != "" {
		raw, err := os.ReadFile(filepath.Clean(configPath))
		if err != nil {
			return fmt.Errorf("read --public-config-file: %w", err)
		}
		publicConfig = raw
	}
	authType, _ := cmd.Flags().GetString("auth-type")
	authHeader, _ := cmd.Flags().GetString("auth-header")
	failurePolicy, _ := cmd.Flags().GetString("failure-policy")
	return remoteMCPAPI(cmd, args, "/config", "configure", map[string]any{
		"endpoint": endpoint, "public_config": publicConfig, "auth_type": authType,
		"auth_header": authHeader, "credential": credential, "failure_policy": failurePolicy,
	})
}

func runPluginRemoteMCPTest(cmd *cobra.Command, args []string) error {
	return remoteMCPAPI(cmd, args, "/test", "test", map[string]any{})
}

func runPluginRemoteMCPApprove(cmd *cobra.Command, args []string) error {
	tools, _ := cmd.Flags().GetStringSlice("tool")
	if len(tools) == 0 {
		return fmt.Errorf("at least one --tool is required")
	}
	return remoteMCPAPI(cmd, args, "/approve", "approve", map[string]any{"tools": tools})
}

func runPluginRemoteMCPRevoke(cmd *cobra.Command, args []string) error {
	return remoteMCPAPI(cmd, args, "/credential", "revoke", nil)
}

func readRemoteMCPCredential(cmd *cobra.Command) (string, error) {
	fromStdin, _ := cmd.Flags().GetBool("credential-stdin")
	fromFile, _ := cmd.Flags().GetString("credential-file")
	if fromStdin && fromFile != "" {
		return "", fmt.Errorf("--credential-stdin and --credential-file are mutually exclusive")
	}
	if !fromStdin && fromFile == "" {
		return "", nil
	}
	var raw []byte
	var err error
	if fromStdin {
		raw, err = io.ReadAll(cmd.InOrStdin())
	} else {
		raw, err = os.ReadFile(filepath.Clean(fromFile))
	}
	if err != nil {
		return "", fmt.Errorf("read Remote MCP credential: %w", err)
	}
	credential := strings.TrimSpace(string(raw))
	if credential == "" {
		return "", fmt.Errorf("Remote MCP credential input is empty")
	}
	return credential, nil
}
