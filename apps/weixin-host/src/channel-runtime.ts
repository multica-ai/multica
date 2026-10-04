import { randomUUID } from "node:crypto";
import fs from "node:fs";
import path from "node:path";

import { MulticaApiError, type MulticaClient } from "./multica-client.ts";
import type { SessionStore } from "./session-store.ts";

/** Longest single Weixin text message the plugin's own outbound path sends. */
const WEIXIN_TEXT_CHUNK_LIMIT = 4000;

export interface ChannelRuntimeDeps {
  client: MulticaClient;
  sessions: SessionStore;
  agentId: string;
  agentName: string;
  stateDir: string;
  replyTimeoutMs: number;
  log: (msg: string) => void;
}

interface TypingCallbacks {
  start(): Promise<void>;
  stop(): Promise<void>;
}

interface ReplyPayload {
  text?: string;
}

interface ReplyDispatcher {
  deliver(payload: ReplyPayload): Promise<void>;
  onError(err: unknown, info: { kind: string }): void;
  typing?: TypingCallbacks;
}

/** The inbound context fields the plugin fills in (see its inbound.ts). */
interface InboundContext {
  Body?: string;
  From?: string;
  AccountId?: string;
  MediaPath?: string;
  MediaType?: string;
  MediaPaths?: string[];
}

/**
 * Implements the `channelRuntime` surface the official openclaw-weixin plugin
 * calls for every inbound message. Where OpenClaw would run its own agent,
 * `dispatchReplyFromConfig` forwards the turn to a Multica agent over the
 * Chat API and hands the agent's answer back to the plugin for delivery.
 */
export function createChannelRuntime(deps: ChannelRuntimeDeps) {
  const { client, sessions, agentId, log } = deps;

  async function ensureSession(accountId: string, peerId: string): Promise<string> {
    const existing = sessions.get(accountId, peerId);
    if (existing) return existing;
    const session = await client.createChatSession(agentId, `微信 · ${shortPeer(peerId)}`);
    sessions.set(accountId, peerId, session.id);
    log(`created Multica chat ${session.id} for ${accountId}/${peerId}`);
    return session.id;
  }

  async function sendToAgent(accountId: string, peerId: string, content: string) {
    const sessionId = await ensureSession(accountId, peerId);
    try {
      const sent = await client.sendChatMessage(sessionId, content);
      return { sessionId, taskId: sent.task_id };
    } catch (err) {
      // The Chat was deleted or archived in Multica: start a fresh one.
      if (err instanceof MulticaApiError && (err.status === 404 || err.status === 409)) {
        sessions.delete(accountId, peerId);
        const fresh = await ensureSession(accountId, peerId);
        const sent = await client.sendChatMessage(fresh, content);
        return { sessionId: fresh, taskId: sent.task_id };
      }
      throw err;
    }
  }

  return {
    routing: {
      resolveAgentRoute({ accountId, peer }: { accountId: string; peer: { id: string } }) {
        const sessionKey = `multica:${agentId}:openclaw-weixin:${accountId}:${peer.id}`;
        return { agentId, sessionKey, mainSessionKey: sessionKey };
      },
    },
    session: {
      resolveStorePath: () => path.join(deps.stateDir, "sessions"),
      // Multica's Chat is the transcript of record; nothing to store here.
      recordInboundSession: async () => {},
    },
    media: {
      async saveMediaBuffer(
        buffer: Buffer,
        _contentType?: string,
        subdir = "inbound",
        maxBytes?: number,
        originalFilename?: string,
      ) {
        if (maxBytes && buffer.length > maxBytes) {
          throw new Error(`media exceeds ${maxBytes} bytes`);
        }
        const dir = path.join(deps.stateDir, "media", subdir);
        fs.mkdirSync(dir, { recursive: true });
        const ext = originalFilename ? path.extname(originalFilename) : "";
        const file = path.join(dir, `${randomUUID()}${ext}`);
        fs.writeFileSync(file, buffer);
        return { path: file };
      },
    },
    // Only read by the host's own command-auth module.
    commands: {},
    reply: {
      finalizeInboundContext: <T>(ctx: T) => ctx,
      resolveHumanDelayConfig: () => undefined,
      createReplyDispatcherWithTyping({
        typingCallbacks,
        deliver,
        onError,
      }: {
        typingCallbacks?: TypingCallbacks;
        deliver: ReplyDispatcher["deliver"];
        onError: ReplyDispatcher["onError"];
      }) {
        const dispatcher: ReplyDispatcher = { deliver, onError, typing: typingCallbacks };
        return {
          dispatcher,
          replyOptions: {},
          markDispatchIdle: () => void typingCallbacks?.stop(),
        };
      },
      withReplyDispatcher: async ({ run }: { run: () => Promise<void> }) => run(),
      async dispatchReplyFromConfig({
        ctx,
        dispatcher,
      }: {
        ctx: InboundContext;
        dispatcher: ReplyDispatcher;
      }) {
        const accountId = ctx.AccountId ?? "";
        const peerId = ctx.From ?? "";
        const content = buildAgentMessage(ctx);
        if (!accountId || !peerId || !content) return;

        await dispatcher.typing?.start();
        try {
          const { sessionId, taskId } = await sendToAgent(accountId, peerId, content);
          log(`→ ${deps.agentName} chat=${sessionId} task=${taskId}`);
          const reply = await client.waitForReply(sessionId, taskId, {
            timeoutMs: deps.replyTimeoutMs,
          });
          const text = reply.failure_reason
            ? `⚠️ ${deps.agentName} 没能完成这次回复：${reply.content || reply.failure_reason}`
            : reply.content;
          log(`← ${deps.agentName} task=${taskId} ${text.length} chars`);
          for (const chunk of chunkText(text, WEIXIN_TEXT_CHUNK_LIMIT)) {
            await dispatcher.deliver({ text: chunk });
          }
        } catch (err) {
          dispatcher.onError(err, { kind: "final" });
        } finally {
          await dispatcher.typing?.stop();
        }
      },
    },
  };
}

/**
 * The agent receives the Weixin text as-is. Media is not forwarded yet, so
 * the agent is told what it is missing instead of silently seeing nothing.
 */
function buildAgentMessage(ctx: InboundContext): string {
  const body = ctx.Body?.trim() ?? "";
  const hasMedia = Boolean(ctx.MediaPath || ctx.MediaPaths?.length);
  if (!hasMedia) return body;
  const note = `[对方通过微信发送了${describeMedia(ctx.MediaType)}，当前版本还不能转发给你]`;
  return body ? `${body}\n\n${note}` : note;
}

function describeMedia(mime?: string): string {
  if (mime?.startsWith("image/")) return "一张图片";
  if (mime?.startsWith("video/")) return "一段视频";
  if (mime?.startsWith("audio/")) return "一段语音";
  return "一个文件";
}

function shortPeer(peerId: string): string {
  const local = peerId.split("@")[0] ?? peerId;
  return local.length > 8 ? local.slice(0, 8) : local;
}

/** Splits on paragraph, then line, then hard boundaries. */
export function chunkText(text: string, limit: number): string[] {
  const chunks: string[] = [];
  let rest = text.trim();
  while (rest.length > limit) {
    const window = rest.slice(0, limit);
    let cut = window.lastIndexOf("\n\n");
    if (cut < limit / 2) cut = window.lastIndexOf("\n");
    if (cut < limit / 2) cut = limit;
    chunks.push(rest.slice(0, cut).trimEnd());
    rest = rest.slice(cut).trimStart();
  }
  if (rest) chunks.push(rest);
  return chunks;
}
