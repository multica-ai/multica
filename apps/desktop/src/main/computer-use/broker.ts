import { z } from "zod";

const identity = z.string().min(1).max(200);
// Every request names the account it acts for, and it must be the account
// signed in to Desktop. Task runs (origin "task") are approved without a dialog.
const scopeSchema = z.object({ userId: identity, workspaceId: identity }).strict();
export const envelopeSchema = z.object({ op: z.enum(["status", "begin", "action", "release"]), conversationId: identity, turnId: identity, scope: scopeSchema, origin: z.literal("task").optional(), action: z.unknown().optional() }).strict();
const point = z.number().finite().nonnegative().max(100000);
const target = { app: identity, observationId: identity };
export const actionSchema = z.discriminatedUnion("op", [
  z.object({ op: z.literal("get_app"), app: identity }).strict(),
  z.object({ op: z.literal("observe"), app: identity }).strict(),
  z.object({ op: z.literal("windows"), app: identity }).strict(),
  z.object({ op: z.literal("select_window"), app: identity, windowId: identity }).strict(),
  z.object({ op: z.literal("click"), ...target, ref: identity.optional(), x: point.optional(), y: point.optional() }).strict(),
  z.object({ op: z.literal("set_value"), ...target, ref: identity, value: z.string().max(10000) }).strict(),
  z.object({ op: z.literal("type"), ...target, text: z.string().min(1).max(10000) }).strict(),
  z.object({ op: z.literal("press"), ...target, key: z.string().min(1).max(80) }).strict(),
  z.object({ op: z.literal("scroll"), ...target, x: point, y: point, dy: z.number().int().min(-3000).max(3000) }).strict(),
  z.object({ op: z.literal("drag"), ...target, x: point, y: point, toX: point, toY: point }).strict(),
]);
const appSchema = z.object({ id: identity, name: identity });
const statusSchema = z.object({ available: z.boolean(), accessibility: z.boolean(), screenRecording: z.boolean() });
export type ComputerEnvelope = z.infer<typeof envelopeSchema>;
export interface BrokerOptions {
  currentUser(): string | null | undefined;
  generation(): number;
  native(action: Record<string, unknown>, signal: AbortSignal): Promise<unknown>;
  confirm(app: { id: string; name: string }, request: ComputerEnvelope, signal: AbortSignal): Promise<boolean>;
  permissions(signal: AbortSignal): Promise<boolean>;
  activity(value: { conversationId: string; app?: string } | null): void;
  stopNative(): void;
  stopped?(request: { conversationId: string; turnId: string }): void;
}

export class ComputerBroker {
  private revoked = new Set<string>();
  private lease?: { key: string; request: ComputerEnvelope; generation: number; controller: AbortController; apps: Set<string> };
  constructor(private options: BrokerOptions) {}
  async handle(raw: unknown, signal: AbortSignal): Promise<unknown> {
    const request = envelopeSchema.parse(raw);
    const { scope, conversationId, turnId, op } = request;
    if (scope.userId !== this.options.currentUser()) throw new Error("account_changed: Computer Use requires the active account");
    signal.throwIfAborted();
    const key = JSON.stringify([scope, conversationId, turnId]);
    if (op === "release") {
      if (this.lease?.key === key) this.stop();
      return { status: "released" };
    }
    if (op === "status") return this.options.native({ op: "status" }, signal);
    if (op === "begin") {
      if (this.revoked.has(key)) throw new Error("execution_ended: The user stopped Computer Use for this turn");
      if (this.lease && this.lease.key !== key) throw new Error("device_busy: Another conversation controls this desktop");
      this.lease ??= { key, request, generation: this.options.generation(), controller: new AbortController(), apps: new Set() };
      const lease = this.lease;
      const status = statusSchema.parse(await this.options.native({ op: "status" }, signal));
      if (!status.accessibility || !status.screenRecording) {
        if (await this.options.permissions(signal)) {
          this.check(key, lease.generation, signal);
          await this.options.native({ op: "request_permissions" }, signal);
        }
        if (this.lease === lease) this.stop();
        throw new Error("permission_required: Grant Screen Recording and Accessibility to Multica in macOS Settings, then retry");
      }
      this.check(key, lease.generation, signal);
      this.options.activity({ conversationId });
      return { status: "ready", mode: "background" };
    }
    const lease = this.lease;
    if (!lease || lease.key !== key) throw new Error("execution_ended: No desktop control lease for this turn");
    const active = AbortSignal.any([signal, lease.controller.signal]);
    this.check(key, lease.generation, active);
    const action = actionSchema.parse(request.action);
    if (action.op === "get_app") {
      const app = appSchema.parse(await this.options.native(action, active));
      if (!lease.apps.has(app.id)) {
        if (request.origin !== "task") {
          if (!await this.options.confirm(app, request, active)) throw new Error("app_not_allowed: Application access was declined");
          this.check(key, lease.generation, active);
        }
        lease.apps.add(app.id);
      }
      this.options.activity({ conversationId, app: app.name });
      await this.options.native({ op: "launch", app: app.id }, active);
      return app;
    }
    if (!lease.apps.has(action.app)) throw new Error("app_not_allowed: Get and approve this application first");
    try { return await this.options.native(action, active); }
    finally { if (active.aborted && this.lease === lease) this.stop(); }
  }
  private check(key: string, generation: number, signal: AbortSignal) {
    if (signal.aborted) throw new Error("execution_ended: Computer Use was cancelled");
    if (this.lease?.key !== key || generation !== this.options.generation()) throw new Error("execution_ended: Computer Use authorization ended");
  }
  revoke() {
    const lease = this.lease;
    if (lease) this.revoked.add(lease.key);
    this.stop();
    if (lease) this.options.stopped?.(lease.request);
  }
  stop() {
    this.lease?.controller.abort();
    this.lease = undefined;
    this.options.stopNative();
    this.options.activity(null);
  }
}
