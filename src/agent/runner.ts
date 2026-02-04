import { Agent as PiAgentCore, type AgentEvent, type AgentMessage } from "@mariozechner/pi-agent-core";
import { v7 as uuidv7 } from "uuid";
import type { AgentOptions, AgentRunResult } from "./types.js";
import { createAgentOutput } from "./cli/output.js";
import { resolveModel, resolveTools } from "./tools.js";
import {
  resolveApiKey,
  resolveApiKeyForProfile,
  resolveApiKeyForProvider,
  resolveBaseUrl,
  resolveModelId,
} from "./providers/index.js";
import { SessionManager } from "./session/session-manager.js";
import { ProfileManager } from "./profile/index.js";
import { SkillManager } from "./skills/index.js";
import { credentialManager, getCredentialsPath } from "./credentials.js";
import {
  checkContextWindow,
  DEFAULT_CONTEXT_TOKENS,
  type ContextWindowGuardResult,
} from "./context-window/index.js";
import { mergeToolsConfig, type ToolsConfig } from "./tools/policy.js";
import {
  loadAuthProfileStore,
  resolveAuthProfileOrder,
  isProfileInCooldown,
  markAuthProfileFailure,
  markAuthProfileUsed,
  markAuthProfileGood,
} from "./auth-profiles/index.js";
import {
  buildSystemPrompt as buildStructuredSystemPrompt,
  collectRuntimeInfo,
  type SystemPromptMode,
} from "./system-prompt/index.js";
import type { AuthProfileFailureReason } from "./auth-profiles/index.js";

// ============================================================
// Error classification for auth profile rotation
// ============================================================

/** Classify an error into an auth profile failure reason */
export function classifyError(error: unknown): AuthProfileFailureReason {
  const msg = error instanceof Error ? error.message.toLowerCase() : String(error).toLowerCase();

  if (msg.includes("401") || msg.includes("403") || msg.includes("unauthorized") || msg.includes("invalid api key") || msg.includes("authentication")) {
    return "auth";
  }
  if (msg.includes("400") || msg.includes("invalid request") || msg.includes("malformed") || msg.includes("bad request") || msg.includes("schema")) {
    return "format";
  }
  if (msg.includes("429") || msg.includes("rate limit") || msg.includes("rate_limit") || msg.includes("too many requests")) {
    return "rate_limit";
  }
  if (msg.includes("billing") || msg.includes("quota") || msg.includes("insufficient") || msg.includes("payment")) {
    return "billing";
  }
  if (msg.includes("timeout") || msg.includes("timed out") || msg.includes("econnreset") || msg.includes("etimedout")) {
    return "timeout";
  }
  return "unknown";
}

/** Check if an error is potentially retryable via profile rotation */
export function isRotatableError(reason: AuthProfileFailureReason): boolean {
  // timeout is rotatable because some providers hang on rate limit instead of returning 429
  return reason === "auth" || reason === "rate_limit" || reason === "billing" || reason === "timeout";
}

export class Agent {
  private readonly agent: PiAgentCore;
  private readonly output;
  private readonly session: SessionManager;
  private readonly profile?: ProfileManager;
  private readonly skillManager?: SkillManager;
  private readonly contextWindowGuard: ContextWindowGuardResult;
  private readonly debug: boolean;
  private toolsOptions: AgentOptions;
  private readonly originalToolsConfig?: ToolsConfig;
  private readonly stderr: NodeJS.WritableStream;
  private initialized = false;

  // Auth profile rotation state
  private readonly resolvedProvider: string;
  private currentApiKey: string | undefined;
  private currentProfileId: string | undefined;
  private profileCandidates: string[];
  private profileIndex: number;
  private readonly pinnedProfile: boolean;

  /** Current session ID */
  readonly sessionId: string;

  constructor(options: AgentOptions = {}) {
    const stdout = options.logger?.stdout ?? process.stdout;
    this.stderr = options.logger?.stderr ?? process.stderr;
    this.output = createAgentOutput({ stdout, stderr: this.stderr });
    this.debug = options.debug ?? false;

    // Resolve provider and model from options > env vars > defaults
    const defaultProvider = options.provider ?? credentialManager.getLlmProvider() ?? "kimi-coding";
    if (options.authProfileId) {
      const profileProvider = options.authProfileId.includes(":")
        ? options.authProfileId.split(":")[0]!
        : options.authProfileId;
      if (options.provider && options.provider !== profileProvider) {
        throw new Error(
          `authProfileId provider mismatch: authProfileId="${options.authProfileId}" ` +
          `does not match provider="${options.provider}"`,
        );
      }
      this.resolvedProvider = profileProvider;
    } else {
      this.resolvedProvider = defaultProvider;
    }
    const resolvedModel = resolveModelId(this.resolvedProvider, options.model);

    // === Auth profile resolution ===
    this.pinnedProfile = !!(options.authProfileId || options.apiKey);

    if (options.apiKey) {
      // Explicit API key — no rotation
      this.currentApiKey = options.apiKey;
      this.currentProfileId = this.resolvedProvider;
      this.profileCandidates = [];
      this.profileIndex = 0;
    } else if (options.authProfileId) {
      // Pinned profile — no rotation
      this.currentApiKey = resolveApiKeyForProfile(options.authProfileId)
        ?? resolveApiKey(this.resolvedProvider);
      this.currentProfileId = options.authProfileId;
      this.profileCandidates = [];
      this.profileIndex = 0;
    } else {
      // Profile-aware resolution with rotation support
      const resolved = resolveApiKeyForProvider(this.resolvedProvider);
      if (resolved) {
        this.currentApiKey = resolved.apiKey;
        this.currentProfileId = resolved.profileId;
      } else {
        this.currentApiKey = undefined;
        this.currentProfileId = undefined;
      }

      // Load full candidate list for rotation
      const store = loadAuthProfileStore();
      this.profileCandidates = resolveAuthProfileOrder(this.resolvedProvider, store);
      this.profileIndex = this.currentProfileId
        ? Math.max(0, this.profileCandidates.indexOf(this.currentProfileId))
        : 0;
    }

    this.agent = new PiAgentCore(
      this.currentApiKey
        ? { getApiKey: (_provider: string) => this.currentApiKey! }
        : {},
    );

    // Load Agent Profile (if profileId is specified)
    // Every Agent should have a Profile for memory, tools config, and other settings
    if (options.profileId) {
      this.profile = new ProfileManager({
        profileId: options.profileId,
        baseDir: options.profileBaseDir,
      });
      // Ensure profile directory exists (creates with default templates if new)
      this.profile.getOrCreateProfile(true);
    }

    // Initialize SkillManager (enabled by default)
    if (options.enableSkills !== false) {
      this.skillManager = new SkillManager({
        profileId: options.profileId,
        profileBaseDir: options.profileBaseDir,
        config: options.skills,
      });
    }

    this.sessionId = options.sessionId ?? uuidv7();

    // 解析 model（用于获取 context window）
    const storedMeta = (() => {
      // 临时创建 session 获取 meta，避免循环依赖
      const tempSession = new SessionManager({ sessionId: this.sessionId });
      return tempSession.getMeta();
    })();

    const effectiveProvider = resolvedModel ? this.resolvedProvider : (options.provider ?? storedMeta?.provider);
    const effectiveModel = resolvedModel ?? options.model ?? storedMeta?.model;
    let model = resolveModel({ ...options, provider: effectiveProvider, model: effectiveModel });

    if (!model) {
      throw new Error(
        `Unknown model: provider="${effectiveProvider}", model="${effectiveModel}". ` +
        `Check ${getCredentialsPath()} for llm.provider and llm.providers.${effectiveProvider}.model.`,
      );
    }

    // Override base URL if provided via options or environment variable
    const baseUrl = resolveBaseUrl(model.provider, options.baseUrl);
    if (baseUrl) {
      model = { ...model, baseUrl };
    }

    // === Context Window Guard ===
    this.contextWindowGuard = checkContextWindow({
      modelContextWindow: model.contextWindow,
      configContextTokens: options.contextWindowTokens,
      defaultTokens: DEFAULT_CONTEXT_TOKENS,
    });

    // 警告：context window 较小
    if (this.contextWindowGuard.shouldWarn) {
      this.stderr.write(
        `[Context Window Guard] WARNING: Low context window: ${this.contextWindowGuard.tokens} tokens (source: ${this.contextWindowGuard.source})\n`,
      );
    }

    // 阻止：context window 太小
    if (this.contextWindowGuard.shouldBlock) {
      throw new Error(
        `[Context Window Guard] Context window too small: ${this.contextWindowGuard.tokens} tokens. ` +
          `Minimum required: 16,000 tokens. Please use a model with a larger context window.`,
      );
    }

    // 确定 compaction 模式
    const compactionMode = options.compactionMode ?? "tokens"; // 默认使用 token 模式

    // 获取 API Key（用于 summary 模式）
    const summaryApiKey = compactionMode === "summary"
      ? resolveApiKey(this.resolvedProvider, options.apiKey)
      : undefined;

    // 创建 SessionManager（带 context window 配置）
    this.session = new SessionManager({
      sessionId: this.sessionId,
      compactionMode,
      // Token 模式参数
      contextWindowTokens: this.contextWindowGuard.tokens,
      // systemPrompt is set later via setSystemPrompt() after tools are resolved
      reserveTokens: options.reserveTokens,
      targetRatio: options.compactionTargetRatio,
      minKeepMessages: options.minKeepMessages,
      // Summary 模式参数
      model: compactionMode === "summary" ? model : undefined,
      apiKey: summaryApiKey,
      customInstructions: options.summaryInstructions,
    });

    if (!options.thinkingLevel && storedMeta?.thinkingLevel) {
      this.agent.setThinkingLevel(storedMeta.thinkingLevel as any);
    } else if (options.thinkingLevel) {
      this.agent.setThinkingLevel(options.thinkingLevel);
    }

    this.agent.setModel(model);

    // Save original tools config from options (for later merging during reload)
    if (options.tools) {
      this.originalToolsConfig = options.tools;
    }

    // Merge Profile tools config with options.tools (options takes precedence)
    const profileToolsConfig = this.profile?.getToolsConfig();
    const mergedToolsConfig = mergeToolsConfig(profileToolsConfig, options.tools);
    this.toolsOptions = mergedToolsConfig ? { ...options, tools: mergedToolsConfig } : options;

    const tools = resolveTools(this.toolsOptions);
    if (this.debug) {
      if (profileToolsConfig) {
        console.error(`[debug] Profile tools config: ${JSON.stringify(profileToolsConfig)}`);
      }
      console.error(`[debug] Merged tools config: ${JSON.stringify(mergedToolsConfig)}`);
      console.error(`[debug] Resolved ${tools.length} tools: ${tools.map(t => t.name).join(", ") || "(none)"}`);
    }
    this.agent.setTools(tools);

    // Build the system prompt using the structured builder
    const toolNames = tools.map((t: { name: string }) => t.name);
    const systemPrompt = this.buildFullSystemPrompt(options, toolNames);
    if (systemPrompt) {
      this.agent.setSystemPrompt(systemPrompt);
      this.session.setSystemPrompt(systemPrompt);
    }

    this.session.saveMeta({
      provider: this.agent.state.model?.provider,
      model: this.agent.state.model?.id,
      thinkingLevel: this.agent.state.thinkingLevel,
      contextWindowTokens: this.contextWindowGuard.tokens,
    });

    this.agent.subscribe((event: AgentEvent) => {
      this.output.handleEvent(event);
      this.handleSessionEvent(event);
    });

    if (this.debug && this.currentProfileId) {
      console.error(`[debug] Auth profile: ${this.currentProfileId} (pinned=${this.pinnedProfile}, candidates=${this.profileCandidates.length})`);
    }
  }

  /** Subscribe to raw AgentEvent from the underlying engine */
  subscribe(fn: (event: AgentEvent) => void): () => void {
    return this.agent.subscribe(fn);
  }

  async run(prompt: string): Promise<AgentRunResult> {
    if (!this.initialized) {
      await this.session.repairIfNeeded((msg) => console.error(msg));
      const restoredMessages = this.session.loadMessages();
      if (restoredMessages.length > 0) {
        if (this.debug) {
          console.error(`[debug] Restoring ${restoredMessages.length} messages from session`);
          for (const msg of restoredMessages) {
            const msgAny = msg as any;
            const content = Array.isArray(msgAny.content)
              ? msgAny.content.map((c: any) => c.type || "text").join(", ")
              : typeof msgAny.content;
            console.error(`[debug]   ${msg.role}: ${content}`);
            if (Array.isArray(msgAny.content)) {
              for (const block of msgAny.content) {
                if (block.type === "tool_use") {
                  console.error(`[debug]     tool_use id: ${block.id}, name: ${block.name}`);
                }
                if (block.type === "tool_result") {
                  console.error(`[debug]     tool_result tool_use_id: ${block.tool_use_id}`);
                }
              }
            }
          }
        }
        this.agent.replaceMessages(restoredMessages);
      }
      this.initialized = true;
    }
    this.output.state.lastAssistantText = "";

    const canRotate = !this.pinnedProfile && this.profileCandidates.length > 1;
    let lastError: unknown;

    // Loop to exhaust all candidate profiles on rotatable errors
    while (true) {
      try {
        await this.agent.prompt(prompt);
        break; // success — exit loop
      } catch (error) {
        lastError = error;

        const reason = classifyError(error);
        if (this.currentProfileId && isRotatableError(reason)) {
          markAuthProfileFailure(this.currentProfileId, reason);
        }

        if (!canRotate || !this.currentProfileId) throw error;
        if (!isRotatableError(reason)) throw error;

        if (this.debug) {
          this.stderr.write(
            `[auth-profile] Profile "${this.currentProfileId}" failed (${reason}), attempting rotation...\n`,
          );
        }

        if (!this.advanceAuthProfile()) {
          throw lastError; // All profiles exhausted
        }

        if (this.debug) {
          this.stderr.write(
            `[auth-profile] Rotated to profile "${this.currentProfileId}"\n`,
          );
        }

        // Reset output for retry
        this.output.state.lastAssistantText = "";
        // continue loop with new profile
      }
    }

    // Mark success
    if (this.currentProfileId) {
      markAuthProfileUsed(this.currentProfileId);
      markAuthProfileGood(this.resolvedProvider, this.currentProfileId);
    }

    return { text: this.output.state.lastAssistantText, error: this.agent.state.error };
  }

  /**
   * Advance to the next non-cooldown auth profile.
   * Returns true if a new profile was activated, false if exhausted.
   */
  private advanceAuthProfile(): boolean {
    const store = loadAuthProfileStore();
    const startIndex = this.profileIndex;

    for (let i = 1; i < this.profileCandidates.length; i++) {
      const nextIndex = (startIndex + i) % this.profileCandidates.length;
      const candidateId = this.profileCandidates[nextIndex] as string | undefined;
      if (!candidateId) continue;

      // Skip profiles in cooldown
      const stats = store.usageStats?.[candidateId];
      if (stats && isProfileInCooldown(stats)) continue;

      // Try to resolve API key
      const apiKey = resolveApiKeyForProfile(candidateId);
      if (!apiKey) continue;

      this.currentApiKey = apiKey;
      this.currentProfileId = candidateId;
      this.profileIndex = nextIndex;
      return true;
    }

    return false;
  }

  private handleSessionEvent(event: AgentEvent) {
    if (event.type === "message_end") {
      const message = event.message as AgentMessage;
      this.session.saveMessage(message);
      if (message.role === "assistant") {
        void this.maybeCompact();
      }
    }
  }

  private async maybeCompact() {
    const messages = this.agent.state.messages.slice();
    const result = await this.session.maybeCompact(messages);
    if (result?.kept) {
      this.agent.replaceMessages(result.kept);
    }
  }

  /**
   * Reload tools from profile config.
   * Call this after updating tool status to apply changes
   * without restarting the agent session.
   */
  reloadTools(): string[] {
    // Re-read profile tools config to get latest changes
    const profileToolsConfig = this.profile?.getToolsConfig();
    console.log(`[Agent] reloadTools: profileToolsConfig =`, JSON.stringify(profileToolsConfig));
    const mergedToolsConfig = mergeToolsConfig(profileToolsConfig, this.originalToolsConfig);
    console.log(`[Agent] reloadTools: mergedToolsConfig =`, JSON.stringify(mergedToolsConfig));
    this.toolsOptions = mergedToolsConfig
      ? { ...this.toolsOptions, tools: mergedToolsConfig }
      : this.toolsOptions;

    const tools = resolveTools(this.toolsOptions);
    console.log(`[Agent] reloadTools: resolved ${tools.length} tools: ${tools.map(t => t.name).join(", ") || "(none)"}`);
    this.agent.setTools(tools);
    if (this.debug) {
      console.error(`[debug] Reloaded ${tools.length} tools: ${tools.map(t => t.name).join(", ") || "(none)"}`);
    }
    return tools.map(t => t.name);
  }

  /** Get current active tool names */
  getActiveTools(): string[] {
    return this.agent.state.tools?.map(t => t.name) ?? [];
  }

  /**
   * Get all skills with their eligibility status.
   * Returns empty array if skills are disabled.
   */
  getSkillsWithStatus(): Array<{
    id: string;
    name: string;
    description: string;
    source: string;
    eligible: boolean;
    reasons?: string[] | undefined;
  }> {
    if (!this.skillManager) {
      return [];
    }
    return this.skillManager.listAllSkillsWithStatus();
  }

  /**
   * Get eligible skills only.
   * Returns empty array if skills are disabled.
   */
  getEligibleSkills(): Array<{
    id: string;
    name: string;
    description: string;
    source: string;
  }> {
    if (!this.skillManager) {
      return [];
    }
    return this.skillManager.listSkills();
  }

  /**
   * Reload skills from disk.
   * Call this after adding/removing skills to apply changes.
   */
  reloadSkills(): void {
    if (this.skillManager) {
      this.skillManager.reload();
    }
  }

  /**
   * Set a tool's enabled status and persist to profile config.
   * Returns the new tools config, or undefined if no profile is loaded.
   */
  setToolStatus(toolName: string, enabled: boolean): { allow?: string[]; deny?: string[] } | undefined {
    if (!this.profile) {
      return undefined;
    }
    const newConfig = this.profile.setToolEnabled(toolName, enabled);
    // Reload tools to apply changes
    this.reloadTools();
    // Build result object, only including defined properties
    const result: { allow?: string[]; deny?: string[] } = {};
    if (newConfig.allow) result.allow = newConfig.allow;
    if (newConfig.deny) result.deny = newConfig.deny;
    return result;
  }

  /**
   * Get current profile ID, if any.
   */
  getProfileId(): string | undefined {
    return this.profile?.getProfile()?.id;
  }

  /**
   * Get agent display name from profile config.
   */
  getAgentName(): string | undefined {
    return this.profile?.getName();
  }

  /**
   * Update agent display name in profile config.
   */
  setAgentName(name: string): void {
    this.profile?.updateName(name);
  }

  /**
   * Get user.md content from profile.
   */
  getUserContent(): string | undefined {
    return this.profile?.getUserContent();
  }

  /**
   * Update user.md content in profile.
   */
  setUserContent(content: string): void {
    this.profile?.updateUserContent(content);
  }

  /**
   * Get agent communication style from profile config.
   */
  getAgentStyle(): string | undefined {
    return this.profile?.getStyle();
  }

  /**
   * Update agent communication style in profile config.
   */
  setAgentStyle(style: string): void {
    this.profile?.updateStyle(style);
  }

  /**
   * Build the full system prompt using the structured builder.
   * Combines profile content, tools, skills, and runtime info.
   */
  private buildFullSystemPrompt(
    options: AgentOptions,
    toolNames: string[],
  ): string | undefined {
    const skillsPrompt = this.skillManager?.buildSkillsPrompt();

    // If a raw systemPrompt is provided directly, use it as-is (backward compat)
    if (!options.profileId && options.systemPrompt) {
      return skillsPrompt
        ? `${options.systemPrompt}\n\n${skillsPrompt}`
        : options.systemPrompt;
    }

    if (!this.profile?.getProfile() && !options.profileId) {
      return skillsPrompt || undefined;
    }

    return this.rebuildSystemPrompt(toolNames);
  }

  /**
   * Reload profile from disk and rebuild system prompt.
   * Call this after updating profile files to apply changes immediately.
   */
  reloadSystemPrompt(): void {
    if (!this.profile) {
      return;
    }

    this.profile.reloadProfile();

    const toolNames = (this.agent.state.tools ?? []).map((t: { name: string }) => t.name);
    const systemPrompt = this.rebuildSystemPrompt(toolNames);

    if (systemPrompt) {
      this.agent.setSystemPrompt(systemPrompt);
      this.session.setSystemPrompt(systemPrompt);
    }
  }

  /**
   * Rebuild system prompt from current state.
   * Shared by constructor (via buildFullSystemPrompt) and reloadSystemPrompt.
   */
  private rebuildSystemPrompt(toolNames: string[]): string | undefined {
    const profile = this.profile?.getProfile();
    if (!profile) return undefined;

    const skillsPrompt = this.skillManager?.buildSkillsPrompt();

    const runtime = collectRuntimeInfo({
      agentName: this.profile?.getName(),
      provider: this.resolvedProvider,
      model: this.agent.state.model?.id,
    });

    return buildStructuredSystemPrompt({
      mode: "full",
      profile: {
        soul: profile.soul,
        user: profile.user,
        workspace: profile.workspace,
        memory: profile.memory,
        config: profile.config,
      },
      profileDir: this.profile!.getProfileDir(),
      tools: toolNames,
      skillsPrompt,
      runtime,
    });
  }
}
