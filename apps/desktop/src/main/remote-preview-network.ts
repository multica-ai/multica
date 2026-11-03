import { app, session, type Session, type WebContents } from "electron";
import { randomUUID } from "node:crypto";
import type { Duplex } from "node:stream";
import { z } from "zod";
import { parseWithFallback } from "@multica/core/api/schema";
import type { LocalConversationScope } from "@multica/core/local-conversation/protocol";
import type { WebPreviewTarget, WebPreviewPhase } from "../shared/resource-preview";
import { previewServiceHost } from "../shared/service-preview-url";
import { LocalV2Client, LocalV2Error } from "./local-conversation-v2";
import { createPreviewProxy, type PreviewDialTarget } from "./preview-proxy";
import { connectPreviewEgress } from "./preview-egress";

const targetSchema = z.object({ deviceId: z.string().min(1).max(200).refine((id) => id !== "local"), conversationId: z.string().min(1).max(160) }).strict();
const leaseSchema = z.object({ version: z.literal(1), service_id: z.string().regex(/^[a-f0-9]{64}$/), capability: z.string().regex(/^[a-f0-9]{64}$/), expires_at: z.string().datetime(), protocol: z.literal("/multica/preview-tcp/1") });
type Lease = z.infer<typeof leaseSchema>;
interface Context {
  key: string; owner: string; target: WebPreviewTarget; scope: LocalConversationScope; leases: Map<string, Promise<Lease>>;
  proxy?: Awaited<ReturnType<typeof createPreviewProxy>>; ready?: Promise<void>; supported?: Promise<void>; refs: number; closed: boolean; renewal?: ReturnType<typeof setInterval>; error?: string;
}
export class RemotePreviewNetwork {
  private contexts = new Map<string, Context>();
  private sessions = new Map<Session, Context>();
  private egress = session.fromPartition(`multica-preview-egress-${randomUUID()}`);
  private login = (event: Electron.Event, contents: WebContents | null, _details: unknown, info: Electron.AuthInfo, callback: (username?: string, password?: string) => void) => {
    const ctx = contents ? this.sessions.get(contents.session) : undefined, proxy = ctx?.proxy;
    if (info.isProxy && proxy && info.host === "127.0.0.1" && String(info.port) === proxy.address.split(":")[1]) { event.preventDefault(); callback(proxy.username, proxy.password); }
  };
  constructor(private client: LocalV2Client, readonly apiOrigin: string, private currentUser: () => string | null | undefined) { app.on("login", this.login); }
  private key(owner: string, target: WebPreviewTarget) { return JSON.stringify([this.apiOrigin, owner, target.deviceId, target.conversationId]); }
  partition(owner: string, target: WebPreviewTarget) { return this.key(owner, targetSchema.parse(target)); }
  error(owner: string, target: WebPreviewTarget) { return this.contexts.get(this.key(owner, target))?.error; }
  private assert(ctx: Context) { if (ctx.closed || this.currentUser() !== ctx.scope.userId) throw new Error("preview_proxy_unavailable: Preview context has closed"); }
  private parse(raw: unknown): Lease {
    const value = parseWithFallback<Lease | null>(raw, leaseSchema, null, { endpoint: "local-v2.preview.services" });
    if (!value) throw new Error("preview_unsupported: Invalid service preview response"); return value;
  }
  private async lease(ctx: Context, target: PreviewDialTarget): Promise<Lease> {
    this.assert(ctx);
    ctx.supported ??= this.client.request(ctx.scope, "GET", "/hello", undefined, ctx.target.deviceId).then((hello) => {
      const value = parseWithFallback<{ capabilities: string[] }>(hello, z.object({ capabilities: z.array(z.string()).default([]) }), { capabilities: [] }, { endpoint: "local-v2.hello" });
      if (!value.capabilities.includes("remote-service-preview-v1")) throw new Error("preview_unsupported: Update the other device to enable service preview");
    }).catch((error) => { ctx.supported = undefined; throw error; });
    await ctx.supported; this.assert(ctx);
    const host = previewServiceHost(target.url); if (!host) throw new Error("preview_destination_denied: Expected an IP or localhost service address");
    const key = `${host}:${target.port}`; const existing = ctx.leases.get(key); if (existing) { const value = await existing; if (Date.parse(value.expires_at) > Date.now()) return value; ctx.leases.delete(key); }
    const pending = this.client.request(ctx.scope, "POST", "/preview/services", { version: 1, request_id: randomUUID(), conversation_id: ctx.target.conversationId, target: { host, port: target.port } }, ctx.target.deviceId).then((raw) => {
      const value = this.parse(raw);
      if (ctx.closed) { void this.client.request(ctx.scope, "DELETE", `/preview/services/${value.service_id}`, undefined, ctx.target.deviceId).catch(() => {}); throw new Error("Preview context closed"); }
      this.assert(ctx); return value;
    }).catch((error) => { if (ctx.leases.get(key) === pending) ctx.leases.delete(key); if (error instanceof LocalV2Error && error.status === 404) throw new Error("preview_unsupported: Update the other device to enable service preview"); throw error; });
    ctx.leases.set(key, pending); return pending;
  }
  private async dial(ctx: Context, target: PreviewDialTarget, signal: AbortSignal): Promise<Duplex> {
    this.assert(ctx);
    try {
      if (!previewServiceHost(target.url)) { const socket = await connectPreviewEgress(target, signal, (url) => this.egress.resolveProxy(url)); ctx.error = undefined; return socket; }
      for (let attempt = 0; ; attempt++) {
        const lease = await this.lease(ctx, target); this.assert(ctx);
        try { const socket = await this.client.previewTunnel(ctx.scope, ctx.target.deviceId, lease.service_id, lease.capability, signal); ctx.error = undefined; return socket; }
        catch (error) {
          // A failed handshake has sent no webpage bytes, so an expired grant may be replaced once.
          if (attempt === 0 && !signal.aborted && error instanceof Error && error.message.startsWith("service_expired:")) { ctx.leases.delete(`${previewServiceHost(target.url)}:${target.port}`); continue; }
          throw error;
        }
      }
    } catch (error) { ctx.error = error instanceof LocalV2Error ? `${error.kind}: ${error.message}` : error instanceof Error ? error.message : "Service preview failed"; throw error; }
  }
  async acquire(owner: string, rawTarget: WebPreviewTarget, url: string, ses: Session, phase: (phase: WebPreviewPhase) => void): Promise<() => void> {
    const target = targetSchema.parse(rawTarget); const key = this.key(owner, target);
    const [userId, workspaceId] = z.tuple([z.string(), z.string()]).parse(JSON.parse(owner));
    let ctx = this.contexts.get(key);
    if (!ctx) { ctx = { key, owner, target, scope: { userId, workspaceId }, leases: new Map(), refs: 0, closed: false }; this.contexts.set(key, ctx); }
    ctx.refs++; const current = ctx;
    try {
      this.assert(current);
      if (!current.ready) current.ready = (async () => {
        phase("connecting_device");
        current.proxy = await createPreviewProxy({ connect: (destination, signal) => this.dial(current, destination, signal), onError: (error) => { current.error = error.message; } });
        if (current.closed) { current.proxy.close(); throw new Error("Preview context closed"); }
        this.sessions.set(ses, current);
        await ses.closeAllConnections();
        this.assert(current);
        await ses.setProxy({ mode: "fixed_servers", proxyRules: `http://${current.proxy.address}`, proxyBypassRules: "<-loopback>" });
        this.assert(current);
        current.renewal = setInterval(() => { for (const [key, pending] of current.leases) void pending.then(async (lease) => {
          const next = this.parse(await this.client.request(current.scope, "POST", `/preview/services/${lease.service_id}/renew`, {}, target.deviceId));
          if (!current.closed) current.leases.set(key, Promise.resolve(next));
        }).catch((error) => { current.error = error instanceof Error ? error.message : "Service lease expired"; current.leases.delete(key); }); }, 10 * 60_000); current.renewal.unref();
      })();
      await current.ready; this.assert(current); phase("connecting_service");
      const parsed = new URL(url);
      if (previewServiceHost(url)) await this.lease(current, { host: parsed.hostname.replace(/^\[|\]$/g, ""), port: Number(parsed.port || (parsed.protocol === "https:" ? 443 : 80)), url });
      this.assert(current); phase("loading");
    } catch (error) { this.release(current); if (error instanceof LocalV2Error) throw new Error(`${error.kind}: ${error.message}`); throw error; }
    let released = false; return () => { if (!released) { released = true; this.release(current); } };
  }
  private release(ctx: Context) {
    if (--ctx.refs > 0 || ctx.closed) return; ctx.closed = true; if (ctx.renewal) clearInterval(ctx.renewal);
    ctx.proxy?.close(); this.contexts.delete(ctx.key);
    for (const [ses, owner] of this.sessions) if (owner === ctx) { this.sessions.delete(ses); void ses.closeAllConnections().catch(() => {}); }
    for (const pending of ctx.leases.values()) void pending.then((lease) => this.client.request(ctx.scope, "DELETE", `/preview/services/${lease.service_id}`, undefined, ctx.target.deviceId)).catch(() => {});
    ctx.leases.clear();
  }
  dispose() { for (const ctx of this.contexts.values()) { ctx.refs = 1; this.release(ctx); } app.removeListener("login", this.login); }
}
