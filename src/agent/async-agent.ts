import { v7 as uuidv7 } from "uuid";
import type { AgentEvent, AgentMessage } from "@mariozechner/pi-agent-core";
import { Agent } from "./runner.js";
import { Channel } from "./channel.js";
import type { AgentOptions, Message } from "./types.js";
import type { MulticaEvent } from "./events.js";

const devNull = { write: () => true } as unknown as NodeJS.WritableStream;

/** Discriminated union of legacy Message, raw AgentEvent, and MulticaEvent */
export type ChannelItem = Message | AgentEvent | MulticaEvent;

export class AsyncAgent {
  private readonly agent: Agent;
  private readonly channel = new Channel<ChannelItem>();
  private _closed = false;
  private queue: Promise<void> = Promise.resolve();
  private closeCallbacks: Array<() => void> = [];
  readonly sessionId: string;

  constructor(options?: AgentOptions) {
    this.agent = new Agent({
      ...options,
      logger: { stdout: devNull, stderr: devNull },
    });
    this.sessionId = this.agent.sessionId;

    // Forward raw AgentEvent and MulticaEvent into the channel
    this.agent.subscribeAll((event: AgentEvent | MulticaEvent) => {
      this.channel.send(event);
    });
  }

  get closed(): boolean {
    return this._closed;
  }

  /** Write message to agent (non-blocking, serialized queue) */
  write(content: string): void {
    if (this._closed) throw new Error("Agent is closed");

    this.queue = this.queue
      .then(async () => {
        if (this._closed) return;
        const result = await this.agent.run(content);
        // Flush pending session writes so waitForIdle() callers
        // can safely read session data from disk.
        await this.agent.flushSession();
        // Normal text is delivered via message_end event; only handle errors here
        if (result.error) {
          this.channel.send({ id: uuidv7(), content: `[error] ${result.error}` });
        }
      })
      .catch((err) => {
        const message = err instanceof Error ? err.message : String(err);
        this.channel.send({ id: uuidv7(), content: `[error] ${message}` });
      });
  }

  /** Continuously read channel stream (AgentEvent + error Messages) */
  read(): AsyncIterable<ChannelItem> {
    return this.channel;
  }

  /**
   * Subscribe to agent events directly (supports multiple subscribers).
   * Unlike read(), this allows multiple consumers to receive the same events.
   * Receives both pi-agent-core AgentEvent and MulticaEvent (e.g. compaction).
   */
  subscribe(callback: (event: AgentEvent | MulticaEvent) => void): () => void {
    console.log(`[AsyncAgent] Adding subscriber for agent: ${this.sessionId}`);
    const unsubscribe = this.agent.subscribeAll((event) => {
      console.log(`[AsyncAgent] Event received: ${event.type}`);
      callback(event);
    });
    return () => {
      console.log(`[AsyncAgent] Removing subscriber for agent: ${this.sessionId}`);
      unsubscribe();
    };
  }

  /** Returns a promise that resolves when the current message queue is drained */
  waitForIdle(): Promise<void> {
    return this.queue;
  }

  /** Register a callback to be invoked when the agent is closed */
  onClose(callback: () => void): void {
    if (this._closed) {
      // Already closed, fire immediately
      callback();
      return;
    }
    this.closeCallbacks.push(callback);
  }

  /** Close agent, stop all reads, fire close callbacks */
  close(): void {
    if (this._closed) return;
    this._closed = true;
    this.channel.close();
    for (const cb of this.closeCallbacks) {
      try {
        cb();
      } catch {
        // Don't let callback errors prevent other callbacks
      }
    }
    this.closeCallbacks = [];
  }

  /** Get current active tool names */
  getActiveTools(): string[] {
    return this.agent.getActiveTools();
  }

  /**
   * Reload tools from credentials config.
   * Call this after updating tool status to apply changes immediately.
   */
  reloadTools(): string[] {
    return this.agent.reloadTools();
  }

  /**
   * Get all skills with their eligibility status.
   */
  getSkillsWithStatus(): Array<{
    id: string;
    name: string;
    description: string;
    source: string;
    eligible: boolean;
    reasons?: string[] | undefined;
  }> {
    return this.agent.getSkillsWithStatus();
  }

  /**
   * Get eligible skills only.
   */
  getEligibleSkills(): Array<{
    id: string;
    name: string;
    description: string;
    source: string;
  }> {
    return this.agent.getEligibleSkills();
  }

  /**
   * Reload skills from disk.
   */
  reloadSkills(): void {
    this.agent.reloadSkills();
  }

  /**
   * Set a tool's enabled status and persist to profile config.
   * Returns the new tools config, or undefined if no profile is loaded.
   */
  setToolStatus(toolName: string, enabled: boolean): { allow?: string[]; deny?: string[] } | undefined {
    return this.agent.setToolStatus(toolName, enabled);
  }

  /**
   * Get current profile ID, if any.
   */
  getProfileId(): string | undefined {
    return this.agent.getProfileId();
  }

  /**
   * Get agent display name from profile config.
   */
  getAgentName(): string | undefined {
    return this.agent.getAgentName();
  }

  /**
   * Update agent display name in profile config.
   */
  setAgentName(name: string): void {
    this.agent.setAgentName(name);
  }

  /**
   * Get user.md content from profile.
   */
  getUserContent(): string | undefined {
    return this.agent.getUserContent();
  }

  /**
   * Update user.md content in profile.
   */
  setUserContent(content: string): void {
    this.agent.setUserContent(content);
  }

  /**
   * Get agent communication style from profile config.
   */
  getAgentStyle(): string | undefined {
    return this.agent.getAgentStyle();
  }

  /**
   * Update agent communication style in profile config.
   */
  setAgentStyle(style: string): void {
    this.agent.setAgentStyle(style);
  }

  /**
   * Reload profile from disk and rebuild system prompt.
   * Call this after updating profile files to apply changes immediately.
   */
  reloadSystemPrompt(): void {
    this.agent.reloadSystemPrompt();
  }

  /** Ensure session messages are loaded from disk (idempotent) */
  async ensureInitialized(): Promise<void> {
    return this.agent.ensureInitialized();
  }

  /**
   * Get all messages from the current session.
   */
  getMessages(): AgentMessage[] {
    return this.agent.getMessages();
  }

  /**
   * Get current provider and model information.
   */
  getProviderInfo(): { provider: string; model: string | undefined } {
    return this.agent.getProviderInfo();
  }

  /**
   * Switch to a different provider and/or model.
   * This updates the agent's model without recreating the session.
   */
  setProvider(providerId: string, modelId?: string): { provider: string; model: string | undefined } {
    return this.agent.setProvider(providerId, modelId);
  }
}
