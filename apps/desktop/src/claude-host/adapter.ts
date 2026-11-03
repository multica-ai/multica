import type { Options, Query, SDKUserMessage, SpareProcess } from "@anthropic-ai/claude-agent-sdk";
import { AdapterError, CLAUDE_ADAPTER_PROTOCOL_VERSION, type AdapterMessage, type DaemonMessage, type DaemonRequest } from "./protocol";

type Sdk = typeof import("@anthropic-ai/claude-agent-sdk");
/** The SDK calls the adapter makes; tests inject a fake. */
export type ClaudeSdk = Pick<Sdk, "query" | "forkSession" | "listSessions" | "getSessionMessages" | "getSessionInfo" | "prewarm">;

export interface AdapterOutput { write(frame: AdapterMessage): Promise<void> }

const DEFAULT_INTERRUPT_DEADLINE_MS = 10_000;
const V = CLAUDE_ADAPTER_PROTOCOL_VERSION;

/** Prompt iterable that stays open across turns of one live Query. */
class PromptQueue implements AsyncIterable<SDKUserMessage> {
  private items: SDKUserMessage[] = [];
  private wake?: () => void;
  private ended = false;
  push(message: SDKUserMessage): void { this.items.push(message); this.wake?.(); }
  end(): void { this.ended = true; this.wake?.(); }
  async *[Symbol.asyncIterator](): AsyncIterator<SDKUserMessage> {
    while (true) {
      const next = this.items.shift();
      if (next) { yield next; continue; }
      if (this.ended) return;
      await new Promise<void>((resolve) => { this.wake = resolve; });
      this.wake = undefined;
    }
  }
}

/**
 * Serves one Claude conversation (or one one-shot request) for the daemon.
 * It only calls the SDK and forwards raw SDK messages; conversation state,
 * storage, MCP and event normalization stay in the daemon.
 */
export class ClaudeAdapter {
  private query?: Query;
  private prompts?: PromptQueue;
  private controller?: AbortController;
  private pump?: Promise<void>;
  private sessionId?: string;
  private generation = 0;
  private closing = false;
  private spare?: SpareProcess;
  /** Settles once a claim was answered; a Query that ends first waits for it. */
  private claiming?: Promise<void>;

  constructor(private sdk: () => Promise<ClaudeSdk>, private output: AdapterOutput) {}

  /** Handles one request and writes exactly one result or error for it. */
  async handle(message: DaemonMessage): Promise<void> {
    try {
      const data = await this.dispatch(message);
      await this.output.write({ version: V, type: "result", id: message.id, ...(data === undefined ? {} : { data }) });
    } catch (error) {
      const failure = error instanceof AdapterError ? error : new AdapterError("sdk_error", error instanceof Error ? error.message : String(error), message.type === "send" ? "outcome_unknown" : undefined);
      await this.output.write({ version: V, type: "error", id: message.id, code: failure.code, message: failure.message, ...(failure.dispatch ? { dispatch: failure.dispatch } : {}) });
    }
  }

  private async dispatch(message: DaemonMessage): Promise<unknown> {
    switch (message.type) {
      case "start": return this.start(message);
      case "prewarm": return this.prewarm(message);
      case "claim": return this.claim(message);
      case "send": return this.send(message);
      case "interrupt": return this.interrupt(message);
      case "set_model": await this.live().setModel(message.model ?? undefined); return undefined;
      case "set_permission_mode": await this.live().setPermissionMode(sdkPermissionMode(message.mode)); return undefined;
      case "apply_flag_settings": await this.live().applyFlagSettings(message.settings as Parameters<Query["applyFlagSettings"]>[0]); return undefined;
      case "set_mcp_servers": return this.live().setMcpServers(message.servers as Parameters<Query["setMcpServers"]>[0]);
      case "stop_task": await this.live().stopTask(message.task_id); return undefined;
      case "answer": throw new AdapterError("unsupported", "interaction answers are not used by this adapter yet");
      case "close": await this.close(); return undefined;
      case "fork_session": {
        const result = await (await this.sdk()).forkSession(message.session_id, {
          ...(message.dir ? { dir: message.dir } : {}), ...(message.title ? { title: message.title } : {}),
          ...(message.up_to_message_id ? { upToMessageId: message.up_to_message_id } : {}),
        });
        return { session_id: result.sessionId };
      }
      case "list_sessions":
        return (await this.sdk()).listSessions({ ...(message.dir ? { dir: message.dir } : {}), ...(message.limit ? { limit: message.limit } : {}), ...(message.offset ? { offset: message.offset } : {}) });
      case "get_session_info":
        return (await (await this.sdk()).getSessionInfo(message.session_id, { ...(message.dir ? { dir: message.dir } : {}) })) ?? null;
      case "get_session_messages":
        return (await this.sdk()).getSessionMessages(message.session_id, { ...(message.dir ? { dir: message.dir } : {}), ...(message.limit ? { limit: message.limit } : {}), ...(message.offset ? { offset: message.offset } : {}) });
      case "supported_models": return this.supportedModels(message);
    }
  }

  private async start(message: DaemonRequest<"start">): Promise<unknown> {
    if (this.query) throw new AdapterError("already_started", "this adapter already serves a conversation");
    const loading = performance.now();
    const sdk = await this.sdk();
    const sdkLoadMs = performance.now() - loading;
    this.prompts = new PromptQueue();
    this.controller = new AbortController();
    this.sessionId = message.resume;
    const options: Options = {
      pathToClaudeCodeExecutable: message.executable, cwd: message.cwd, abortController: this.controller,
      includePartialMessages: true, settingSources: ["user", "project", "local"], plugins: [], strictMcpConfig: true,
      // Only unlocks switching to bypassPermissions, including mid-session; permissionMode decides what applies.
      allowDangerouslySkipPermissions: true,
      ...(message.env ? { env: message.env } : {}),
      ...(message.settings ? { settings: message.settings as Options["settings"] } : {}),
      ...(message.resume ? { resume: message.resume } : {}),
      ...(message.model ? { model: message.model } : {}),
      ...(message.permission_mode && message.permission_mode !== "default" ? { permissionMode: sdkPermissionMode(message.permission_mode) } : {}),
      ...(message.append_system_prompt ? { systemPrompt: { type: "preset", preset: "claude_code", append: message.append_system_prompt, snapshot: false } } : {}),
      ...(message.reasoning_effort ? { effort: claudeEffort(message.reasoning_effort) } : {}),
      mcpServers: (message.mcp_servers ?? {}) as Options["mcpServers"],
      ...(message.allowed_tools ? { allowedTools: message.allowed_tools } : {}),
    };
    this.query = sdk.query({ prompt: this.prompts, options });
    this.generation++;
    this.pump = this.consume(this.query, this.generation);
    void this.pump.catch(() => {});
    // Initialization completes the handshake; failures surface to the start request.
    const initializing = performance.now();
    const models = await this.query.supportedModels();
    return { models, timing: { sdk_load_ms: sdkLoadMs, query_init_ms: performance.now() - initializing } };
  }

  /** Parks a Claude Code process with everything a session cannot change later; it serves no session until claimed. */
  private async prewarm(message: DaemonRequest<"prewarm">): Promise<unknown> {
    if (this.query || this.spare) throw new AdapterError("already_started", "this adapter already serves a conversation");
    const loading = performance.now();
    const sdk = await this.sdk();
    const sdkLoadMs = performance.now() - loading;
    if (typeof sdk.prewarm !== "function") throw new AdapterError("prewarm_unsupported", "this Agent SDK cannot prewarm");
    const options: Options = {
      pathToClaudeCodeExecutable: message.executable, includePartialMessages: true, settingSources: ["user", "project", "local"],
      plugins: [], strictMcpConfig: true, mcpServers: {}, allowDangerouslySkipPermissions: true,
      systemPrompt: { type: "preset", preset: "claude_code", snapshot: false },
      ...(message.env ? { env: message.env } : {}),
      ...(message.settings ? { settings: message.settings as Options["settings"] } : {}),
      ...(message.allowed_tools ? { allowedTools: message.allowed_tools } : {}),
    };
    const warming = performance.now();
    let spare: SpareProcess;
    try { spare = await sdk.prewarm({ options }); }
    catch (error) {
      const text = error instanceof Error ? error.message : String(error);
      throw new AdapterError(/await-claim/.test(text) ? "prewarm_unsupported" : "prewarm_failed", text);
    }
    this.spare = spare;
    void spare.exited.then(async () => {
      if (this.spare === spare && !this.closing) await this.output.write({ version: V, type: "error", code: "spare_exited", message: "the parked Claude Code process exited" });
    });
    return { timing: { sdk_load_ms: sdkLoadMs, prewarm_ms: performance.now() - warming } };
  }

  /**
   * Binds the parked process to a session with no prompt queued, so nothing has
   * reached the model when this fails. Readiness is the claimed promise:
   * supportedModels() keeps describing the parked process after a refusal.
   * Refusal output is withheld, so the daemon only sees a typed not_claimed.
   */
  private async claim(message: DaemonRequest<"claim">): Promise<unknown> {
    const spare = this.spare;
    if (!spare || this.query) throw new AdapterError("not_claimed", "no spare process is parked");
    this.spare = undefined;
    const prompts = new PromptQueue();
    let settle = () => {};
    this.claiming = new Promise((resolve) => { settle = resolve; });
    const claimingAt = performance.now();
    try {
      const query = spare.claim({ prompt: prompts, options: {
        cwd: message.cwd,
        ...(message.model ? { model: message.model } : {}),
        ...(message.permission_mode && message.permission_mode !== "default" ? { permissionMode: sdkPermissionMode(message.permission_mode) } : {}),
        ...(message.append_system_prompt ? { appendSystemPrompt: message.append_system_prompt } : {}),
        ...(message.settings ? { settings: message.settings as Parameters<Query["applyFlagSettings"]>[0] } : {}),
      } });
      this.query = query;
      this.prompts = prompts;
      this.generation++;
      this.pump = this.consume(query, this.generation);
      void this.pump.catch(() => {});
      const claimed = await spare.claimed;
      this.sessionId = claimed.sessionId;
      return { session_id: claimed.sessionId, timing: { claim_ms: performance.now() - claimingAt } };
    } catch (error) {
      this.query = undefined;
      this.prompts = undefined;
      prompts.end();
      spare.close();
      throw new AdapterError("not_claimed", error instanceof Error ? error.message : String(error));
    } finally {
      settle();
      this.claiming = undefined;
    }
  }

  private send(message: DaemonRequest<"send">): undefined {
    if (!this.query || !this.prompts || this.closing) throw new AdapterError("not_started", "start the conversation before sending", "not_dispatched");
    this.prompts.push({
      type: "user", uuid: message.request_id as SDKUserMessage["uuid"], session_id: this.sessionId ?? "",
      parent_tool_use_id: null, message: { role: "user", content: message.content as never },
    });
    return undefined;
  }

  /** Cooperative interrupt first; if Claude Code does not acknowledge in time, close the Query transport. */
  private async interrupt(message: DaemonRequest<"interrupt">): Promise<unknown> {
    const query = this.live();
    const deadline = message.deadline_ms ?? DEFAULT_INTERRUPT_DEADLINE_MS;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const timedOut = new Promise<"timeout">((resolve) => { timer = setTimeout(() => resolve("timeout"), deadline); });
    try {
      const outcome = await Promise.race([query.interrupt().then(() => "acknowledged" as const), timedOut]);
      if (outcome === "timeout") { query.close(); return { outcome: "closed" }; }
      return { outcome };
    } finally { clearTimeout(timer); }
  }

  private async supportedModels(message: DaemonRequest<"supported_models">): Promise<unknown> {
    const prompts = new PromptQueue();
    const query = (await this.sdk()).query({ prompt: prompts, options: {
      pathToClaudeCodeExecutable: message.executable, settingSources: [],
      settings: { disableAllHooks: true, enabledPlugins: {}, syncClaudeAiPlugins: false } as Options["settings"],
      plugins: [], tools: [], strictMcpConfig: true, mcpServers: {}, persistSession: false,
    } });
    try { return { models: await query.supportedModels() }; } finally { prompts.end(); query.close(); }
  }

  /** Ends the prompt stream so Claude Code persists its transcript, then stops the Query. */
  async close(): Promise<void> {
    if (this.closing) return;
    this.closing = true;
    this.prompts?.end();
    this.query?.close();
    this.spare?.close();
    await this.pump?.catch(() => {});
  }

  private live(): Query {
    if (!this.query || this.closing) throw new AdapterError("not_started", "the conversation is not running");
    return this.query;
  }

  private async consume(query: Query, generation: number): Promise<void> {
    try {
      for await (const message of query) {
        if (message.type === "system" && message.subtype === "init") this.sessionId = message.session_id;
        // Awaiting each write applies backpressure to the SDK iterator, and so to Claude Code.
        await this.output.write({ version: V, type: "sdk_message", query_generation: generation, message: message as unknown as { type: string } & Record<string, unknown> });
      }
      await this.claiming;
      if (!this.closing && this.query === query) await this.output.write({ version: V, type: "error", code: "query_ended", message: "Claude Code ended the session" });
    } catch (error) {
      await this.claiming;
      if (!this.closing && this.query === query) await this.output.write({ version: V, type: "error", code: "sdk_error", message: error instanceof Error ? error.message : String(error) });
    } finally {
      if (this.query === query) { this.query = undefined; this.prompts = undefined; }
    }
  }
}

function sdkPermissionMode(mode: "default" | "auto" | "full-access"): NonNullable<Options["permissionMode"]> {
  return mode === "full-access" ? "bypassPermissions" : mode === "auto" ? "auto" : "default";
}

function claudeEffort(value: string): NonNullable<Options["effort"]> {
  if (value === "low" || value === "medium" || value === "high" || value === "xhigh" || value === "max") return value;
  throw new AdapterError("invalid_request", "Invalid Claude thinking effort");
}
