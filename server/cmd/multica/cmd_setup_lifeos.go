package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	defaultLifeOSProfile    = "lifeos"
	lifeOSAgentName         = "AI 星耀"
	lifeOSJudgeName         = "LifeOS Judge"
	lifeOSExecutionModel    = "gpt-5.6-sol"
	lifeOSExecutionThinking = "high"
	lifeOSJudgeModel        = "gpt-5.6-sol"
	lifeOSJudgeThinking     = "xhigh"
	lifeOSJudgeInstructions = `你是 LifeOS Judge。先调用 LifeOS MCP 的 context_prepare，独立审核 AI 星耀的结果，核对任务成功标准、事实证据、测试结果、隐私边界与未覆盖风险，不替执行者粉饰结论。

用户可见评论必须讲人话并且只发一条：第一句直接说“我检查完了：可以验收”“我检查完了：需要返工”或“我检查完了：需要你决定”；随后最多使用“我确认了什么”“还需要注意什么”“你现在需要做什么”三段。董事长无需处理时明确写“你现在不用处理”。除非某项内部技术信息本身会改变董事长的判断或下一动作，不得在用户可见评论中出现 UUID、评论 ID、提交哈希、文件路径、运行键、字段名、模型名、状态枚举或完整测试清单。

先用 multica issue comment add 以你自己的 Judge 身份发布上述白话结论，再调用 review_submit 给出 pass、rework 或 needs_chairman。详细技术证据只放在 review_submit 的 evidence 和 gaps 中，用于审计与状态交接；review_submit 不代写第二条用户评论。pass 只提交董事长最终验收，不代表你可以写入 done；rework 会退回 AI 星耀继续执行。外部发送、公开发布、付款、删除、权限或生产变更必须等待董事长确认。`
)

var setupLifeOSCmd = &cobra.Command{
	Use:   "lifeos",
	Short: "Configure the private local LifeOS workbench",
	Long: `Connects the local-only LifeOS workbench without browser login,
starts the Codex daemon, and idempotently provisions AI 星耀 and its Judge.

The server must advertise LIFEOS_LOCAL_MODE=true and use a loopback URL. The
default named profile is "lifeos", so existing Multica CLI state is untouched.`,
	RunE: runSetupLifeOS,
}

func init() {
	setupLifeOSCmd.Flags().String("server-url", "http://localhost:8080", "LifeOS backend URL (loopback only)")
	setupLifeOSCmd.Flags().String("app-url", "http://localhost:3000", "LifeOS workbench URL")
	setupLifeOSCmd.Flags().String("lifeos-root", "", "LifeOS AI root (env: LIFEOS_ROOT)")
	setupLifeOSCmd.Flags().String("controller-root", "", "LifeOS controller code root (env: LIFEOS_CONTROLLER_ROOT; defaults to LifeOS root)")
	setupLifeOSCmd.Flags().Bool("no-start", false, "Configure only; do not start the daemon or provision agents")
	setupCmd.AddCommand(setupLifeOSCmd)
}

type lifeOSPublicConfig struct {
	LocalMode           bool `json:"local_mode"`
	LocalAuthConfigured bool `json:"local_auth_configured"`
}

type lifeOSLocalSession struct {
	Token string `json:"token"`
	User  struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"user"`
	Workspace struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	} `json:"workspace"`
}

type lifeOSRuntime struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Status   string `json:"status"`
}

type lifeOSAgent struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	RuntimeID  string  `json:"runtime_id"`
	ArchivedAt *string `json:"archived_at"`
}

type lifeOSCommentReconcileResponse struct {
	Scanned   int `json:"scanned"`
	Queued    int `json:"queued"`
	Coalesced int `json:"coalesced"`
	Deferred  int `json:"deferred"`
	Blocked   int `json:"blocked"`
}

func runSetupLifeOS(cmd *cobra.Command, _ []string) error {
	if resolveProfile(cmd) == "" {
		if err := cmd.Flags().Set("profile", defaultLifeOSProfile); err != nil {
			return fmt.Errorf("select LifeOS profile: %w", err)
		}
	}
	profile := resolveProfile(cmd)
	serverURL, _ := cmd.Flags().GetString("server-url")
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	appURL, _ := cmd.Flags().GetString("app-url")
	appURL = strings.TrimRight(strings.TrimSpace(appURL), "/")
	if !serverHostIsLocal(serverURL) {
		return fmt.Errorf("LifeOS local mode only accepts a loopback backend URL (got %q)", serverURL)
	}
	if !probeServer(serverURL) {
		return fmt.Errorf("LifeOS backend is not reachable at %s", serverURL)
	}

	ctx, cancel := cli.APIContext(context.Background())
	cfg, identity, err := bootstrapLifeOSProfile(ctx, serverURL, appURL, profile)
	cancel()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "LifeOS local session ready for %s (%s).\n", identity.Name, identity.Email)
	printConfigLocation(profile)

	noStart, _ := cmd.Flags().GetBool("no-start")
	if noStart {
		return nil
	}
	if _, err := exec.LookPath("codex"); err != nil {
		return fmt.Errorf("Codex CLI is required but was not found on PATH")
	}

	healthCtx, healthCancel := context.WithTimeout(context.Background(), 2*time.Second)
	health := checkDaemonHealthOnPort(healthCtx, healthPortForProfile(profile))
	healthCancel()
	if !daemonAlive(health) {
		fmt.Fprintln(os.Stderr, "Starting the private LifeOS Codex runtime...")
		if err := runDaemonBackground(cmd); err != nil {
			return fmt.Errorf("start LifeOS daemon: %w", err)
		}
	}

	lifeOSRoot, err := resolveLifeOSRoot(cmd)
	if err != nil {
		return err
	}
	controllerRoot, err := resolveLifeOSControllerRoot(cmd, lifeOSRoot)
	if err != nil {
		return err
	}
	agentCtx, agentCancel := cli.APIContext(context.Background())
	defer agentCancel()
	if err := ensureLifeOSAgents(agentCtx, cfg, lifeOSRoot, controllerRoot); err != nil {
		return err
	}
	reconciled, err := reconcileLifeOSComments(agentCtx, cfg)
	if err != nil {
		return err
	}
	if reconciled.Blocked > 0 {
		return fmt.Errorf("AI 星耀 could not accept %d recovered comments", reconciled.Blocked)
	}
	accepted := reconciled.Queued + reconciled.Coalesced + reconciled.Deferred
	if accepted > 0 {
		fmt.Fprintf(os.Stderr, "AI 星耀 recovered %d recent comments.\n", accepted)
	}
	fmt.Fprintf(os.Stderr, "LifeOS workbench is ready: %s/lifeos/issues\n", appURL)
	return nil
}

func reconcileLifeOSComments(ctx context.Context, cfg cli.CLIConfig) (lifeOSCommentReconcileResponse, error) {
	client := cli.NewAPIClient(cfg.ServerURL, cfg.WorkspaceID, cfg.Token)
	var response lifeOSCommentReconcileResponse
	if err := client.PostJSON(ctx, "/api/lifeos/comments/reconcile", map[string]any{
		"lookback_hours": 72,
		"limit":          100,
	}, &response); err != nil {
		return lifeOSCommentReconcileResponse{}, fmt.Errorf("reconcile recent LifeOS comments: %w", err)
	}
	return response, nil
}

func resolveLifeOSControllerRoot(cmd *cobra.Command, lifeOSRoot string) (string, error) {
	root, _ := cmd.Flags().GetString("controller-root")
	if root == "" {
		root = strings.TrimSpace(os.Getenv("LIFEOS_CONTROLLER_ROOT"))
	}
	if root == "" {
		root = lifeOSRoot
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve LifeOS controller root: %w", err)
	}
	script := filepath.Join(abs, "scripts", "lifeos_mcp_server.py")
	if info, err := os.Stat(script); err != nil || info.IsDir() {
		return "", fmt.Errorf("LifeOS controller script was not found: %s", script)
	}
	return abs, nil
}

type lifeOSIdentity struct {
	Name  string
	Email string
}

func bootstrapLifeOSProfile(
	ctx context.Context,
	serverURL string,
	appURL string,
	profile string,
) (cli.CLIConfig, lifeOSIdentity, error) {
	publicClient := cli.NewAPIClient(serverURL, "", "")
	var publicConfig lifeOSPublicConfig
	if err := publicClient.GetJSON(ctx, "/api/config", &publicConfig); err != nil {
		return cli.CLIConfig{}, lifeOSIdentity{}, fmt.Errorf("read LifeOS server mode: %w", err)
	}
	if !publicConfig.LocalMode {
		return cli.CLIConfig{}, lifeOSIdentity{}, fmt.Errorf("server at %s is not running with LIFEOS_LOCAL_MODE=true", serverURL)
	}

	existing, _ := cli.LoadCLIConfigForProfile(profile)
	if existing.Token != "" && strings.TrimRight(existing.ServerURL, "/") == serverURL {
		client := cli.NewAPIClient(serverURL, existing.WorkspaceID, existing.Token)
		var me struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		if err := client.GetJSON(ctx, "/api/me", &me); err == nil && existing.WorkspaceID != "" {
			existing.AppURL = appURL
			applyLifeOSDaemonDefaults(&existing)
			if err := cli.SaveCLIConfigForProfile(existing, profile); err != nil {
				return cli.CLIConfig{}, lifeOSIdentity{}, fmt.Errorf("save LifeOS profile: %w", err)
			}
			return existing, lifeOSIdentity{Name: me.Name, Email: me.Email}, nil
		}
	}

	automationToken, err := readLifeOSAutomationToken()
	if err != nil {
		return cli.CLIConfig{}, lifeOSIdentity{}, err
	}
	publicClient.ExtraHeaders = map[string]string{
		"X-LifeOS-Automation-Token": automationToken,
	}
	var session lifeOSLocalSession
	if err := publicClient.PostJSON(ctx, "/auth/local/automation", map[string]any{}, &session); err != nil {
		return cli.CLIConfig{}, lifeOSIdentity{}, fmt.Errorf("create LifeOS local session: %w", err)
	}
	if session.Token == "" || session.Workspace.ID == "" {
		return cli.CLIConfig{}, lifeOSIdentity{}, fmt.Errorf("LifeOS local session response was incomplete")
	}

	jwtClient := cli.NewAPIClient(serverURL, session.Workspace.ID, session.Token)
	var pat struct {
		Token string `json:"token"`
	}
	if err := jwtClient.PostJSON(ctx, "/api/tokens", map[string]any{
		"name":            "LifeOS local daemon",
		"expires_in_days": 365,
	}, &pat); err != nil {
		return cli.CLIConfig{}, lifeOSIdentity{}, fmt.Errorf("issue LifeOS daemon token: %w", err)
	}
	if pat.Token == "" {
		return cli.CLIConfig{}, lifeOSIdentity{}, fmt.Errorf("LifeOS daemon token response was empty")
	}

	cfg := existing
	cfg.ServerURL = serverURL
	cfg.AppURL = appURL
	cfg.WorkspaceID = session.Workspace.ID
	cfg.Token = pat.Token
	applyLifeOSDaemonDefaults(&cfg)
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return cli.CLIConfig{}, lifeOSIdentity{}, fmt.Errorf("save LifeOS profile: %w", err)
	}
	return cfg, lifeOSIdentity{Name: session.User.Name, Email: session.User.Email}, nil
}

func readLifeOSAutomationToken() (string, error) {
	path := strings.TrimSpace(os.Getenv("LIFEOS_AUTOMATION_TOKEN_FILE"))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve LifeOS automation credential: %w", err)
		}
		path = filepath.Join(home, "Library", "Application Support", "LifeOS", "secrets", "automation-token")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read LifeOS automation credential: %w", err)
	}
	token := strings.TrimSpace(string(payload))
	if len(token) < 32 {
		return "", fmt.Errorf("LifeOS automation credential is invalid")
	}
	return token, nil
}

func applyLifeOSDaemonDefaults(cfg *cli.CLIConfig) {
	cfg.DeviceName = "LifeOS"
	cfg.RuntimeName = "AI 星耀本地执行器"
	cfg.MaxConcurrentTasks = 3
	cfg.PollInterval = "2s"
	cfg.DisableAutoUpdate = true
}

func resolveLifeOSRoot(cmd *cobra.Command) (string, error) {
	root, _ := cmd.Flags().GetString("lifeos-root")
	if root == "" {
		root = strings.TrimSpace(os.Getenv("LIFEOS_ROOT"))
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve LifeOS root: %w", err)
		}
		root = filepath.Join(home, "Documents", "Life OS AI")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve LifeOS root: %w", err)
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return "", fmt.Errorf("LifeOS root is not a directory: %s", abs)
	}
	return abs, nil
}

type lifeOSAgentSpec struct {
	Name               string
	Role               string
	Description        string
	Instructions       string
	Model              string
	ThinkingLevel      string
	MaxConcurrentTasks int
}

func defaultLifeOSAgentSpecs() []lifeOSAgentSpec {
	return []lifeOSAgentSpec{
		{
			Name:               lifeOSAgentName,
			Role:               "ceo",
			Description:        "LifeOS CEO：理解全局、接单、执行、暴露阻塞并推进闭环。",
			Instructions:       `你是 AI 星耀，陈星耀的 LifeOS CEO。每次接到任务，先调用 LifeOS MCP 的 context_prepare 获取任务简报和必要上下文，再真正执行任务，而不只给建议。保持事实、推测和工作标签可区分；不得把私密原文、密钥或 Token 写入 LifeOS。所有进展、验证证据和阻塞问题都必须先用 multica issue comment add 以你自己的 Agent 身份发布，再调用 LifeOS MCP 改变状态；MCP 只负责状态与交接，不代你写评论。缺少关键信息时先由秘书准备并核实，不能仅因缺少回执或负责人是董事长就转为本人待办。确实需要董事长判断且事实、建议、材料入口与下一步齐备时，才先发布具体问题和已完成排查，再通过 task_update_or_resume 把任务转为 blocked（需要我）。已配置专业秘书视图时，遵循 workflows/professional-secretary.md；普通运行、资料和讨论不新增本人待办，已完成阶段和明确取消不得复活。完成执行后，先发布结果与验证证据，再通过 task_update_or_resume 把任务转为 in_review（待验收），由 Judge 独立复核；不要自行宣告最终验收，也不得写入 done。外部发送、公开发布、付款、删除、权限或生产变更必须等待董事长确认。`,
			Model:              lifeOSExecutionModel,
			ThinkingLevel:      lifeOSExecutionThinking,
			MaxConcurrentTasks: 2,
		},
		{
			Name:               lifeOSJudgeName,
			Role:               "judge",
			Description:        "LifeOS 独立审核：核对证据、边界、完成标准与战略适配。",
			Instructions:       lifeOSJudgeInstructions,
			Model:              lifeOSJudgeModel,
			ThinkingLevel:      lifeOSJudgeThinking,
			MaxConcurrentTasks: 1,
		},
	}
}

func ensureLifeOSAgents(ctx context.Context, cfg cli.CLIConfig, lifeOSRoot, controllerRoot string) error {
	client := cli.NewAPIClient(cfg.ServerURL, cfg.WorkspaceID, cfg.Token)
	var runtimes []lifeOSRuntime
	if err := client.GetJSON(ctx, "/api/runtimes", &runtimes); err != nil {
		return fmt.Errorf("list LifeOS runtimes: %w", err)
	}
	runtimeID := ""
	for _, runtime := range runtimes {
		if runtime.Provider == "codex" && runtime.Status == "online" {
			runtimeID = runtime.ID
			break
		}
	}
	if runtimeID == "" {
		return fmt.Errorf("LifeOS daemon is online but did not register a Codex runtime")
	}

	var agents []lifeOSAgent
	if err := client.GetJSON(ctx, "/api/agents?include_archived=true", &agents); err != nil {
		return fmt.Errorf("list LifeOS agents: %w", err)
	}
	byName := make(map[string]lifeOSAgent, len(agents))
	for _, item := range agents {
		byName[item.Name] = item
	}

	codexHome := strings.TrimSpace(os.Getenv("LIFEOS_CODEX_HOME"))
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve Codex home: %w", err)
		}
		codexHome = filepath.Join(home, ".codex")
	}
	workbenchDB := strings.TrimSpace(os.Getenv("LIFEOS_WORKBENCH_DB"))
	if workbenchDB == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve LifeOS workbench state: %w", err)
		}
		workbenchDB = filepath.Join(home, "Library", "Application Support", "LifeOS", "data", "lifeos-workbench.sqlite3")
	}
	specs := defaultLifeOSAgentSpecs()

	for _, spec := range specs {
		existing, found := byName[spec.Name]
		if found && existing.ArchivedAt != nil {
			if err := client.PostJSON(ctx, "/api/agents/"+existing.ID+"/restore", map[string]any{}, &existing); err != nil {
				return fmt.Errorf("restore %s: %w", spec.Name, err)
			}
		}
		body := map[string]any{
			"runtime_id":           runtimeID,
			"description":          spec.Description,
			"instructions":         spec.Instructions,
			"model":                spec.Model,
			"thinking_level":       spec.ThinkingLevel,
			"mcp_config":           lifeOSMCPConfig(lifeOSRoot, controllerRoot, cfg.ServerURL, codexHome, workbenchDB, spec.Role),
			"max_concurrent_tasks": spec.MaxConcurrentTasks,
		}
		if found {
			if err := client.PutJSON(ctx, "/api/agents/"+existing.ID, body, &existing); err != nil {
				return fmt.Errorf("update %s: %w", spec.Name, err)
			}
			continue
		}
		body["name"] = spec.Name
		body["permission_mode"] = "private"
		var created lifeOSAgent
		if err := client.PostJSON(ctx, "/api/agents", body, &created); err != nil {
			return fmt.Errorf("create %s: %w", spec.Name, err)
		}
	}
	return nil
}

func lifeOSMCPConfig(lifeOSRoot, controllerRoot, serverURL, codexHome, workbenchDB, role string) map[string]any {
	return map[string]any{
		"mcpServers": map[string]any{
			"lifeos": map[string]any{
				"command": "python3",
				"args":    []string{filepath.Join(controllerRoot, "scripts", "lifeos_mcp_server.py")},
				"env": map[string]string{
					"LIFEOS_ROOT":         lifeOSRoot,
					"LIFEOS_SERVER_URL":   serverURL,
					"LIFEOS_CODEX_HOME":   codexHome,
					"LIFEOS_WORKBENCH_DB": workbenchDB,
					"LIFEOS_ROLE":         role,
				},
			},
		},
	}
}
